// Command api is the Homend Residence backend: the HTTP server, database migrations and admin commands.
//
//	api serve                          run the HTTP server (default)
//	api migrate up|down|status|reset   apply the embedded goose migrations
//	api create-staff -email … -name … -roles admin,triage_officer
//	api seed-demo                      load the demo catalogue (refused when APP_ENV=prod)
package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/config"
	"github.com/KING-CYBERTON/home-residency/backend/internal/crypt"
	"github.com/KING-CYBERTON/home-residency/backend/internal/db"
	"github.com/KING-CYBERTON/home-residency/backend/internal/email"
	"github.com/KING-CYBERTON/home-residency/backend/internal/mpesa"
	"github.com/KING-CYBERTON/home-residency/backend/internal/sms"
	"github.com/KING-CYBERTON/home-residency/backend/internal/storage"
)

var version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	if err := run(cmd, args); err != nil {
		slog.Error("fatal", "err", err)
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "migrate":
		if len(args) != 1 {
			return errors.New("usage: api migrate up|down|status|reset")
		}
		return db.Migrate(ctx, cfg.DatabaseURL, args[0])
	case "serve":
		return serve(ctx, cfg)
	case "create-staff":
		return createStaff(ctx, cfg, args)
	case "seed-demo":
		return seedDemo(ctx, cfg)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func newServer(ctx context.Context, cfg config.Config) (*api.Server, func(), error) {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	cipher, err := crypt.NewAESGCM(cfg.FieldKey)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	api.Version = version
	pay, err := mpesa.New(mpesa.Config{Mode: cfg.MpesaMode, ConsumerKey: cfg.MpesaKey, ConsumerSecret: cfg.MpesaSecret, ShortCode: cfg.MpesaShortCode,
		PassKey: cfg.MpesaPassKey, Initiator: cfg.MpesaInitiator, SecurityCred: cfg.MpesaSecurity, B2CShortCode: cfg.MpesaB2CCode})
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	var store storage.Store
	switch cfg.StorageDriver {
	case "s3":
		store, err = storage.NewS3(ctx, cfg.StorageBucket, cfg.StorageKMSKey)
	default:
		store = &storage.Local{Dir: cfg.StorageDir, BaseURL: cfg.PublicBaseURL, Secret: crypt.SHA256("storage:" + cfg.FieldKey)}
	}
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	mail, err := email.New(ctx, cfg.EmailProvider, cfg.EmailFrom)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	srv := api.NewServer(pool, cfg, sms.New(cfg.SMSProvider), cipher, pay, store)
	srv.Email = mail
	return srv, pool.Close, nil
}

func serve(ctx context.Context, cfg config.Config) error {
	srv, closeDB, err := newServer(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB()
	if err := mountWeb(srv, cfg); err != nil {
		return err
	}
	h, err := srv.Handler()
	if err != nil {
		return err
	}
	httpSrv := &http.Server{Addr: cfg.HTTPAddr, Handler: h, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 2 * time.Minute}
	go srv.RunSweepers(ctx)
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	slog.Info("listening", "addr", cfg.HTTPAddr, "env", cfg.Env, "version", version)
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}

func createStaff(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("create-staff", flag.ContinueOnError)
	email := fs.String("email", "", "staff email")
	name := fs.String("name", "", "full name")
	roles := fs.String("roles", "", "comma-separated roles, e.g. admin,triage_officer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" || *name == "" || *roles == "" {
		return errors.New("-email, -name and -roles are required")
	}
	srv, closeDB, err := newServer(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB()
	pw, secret, uri, err := srv.CreateStaff(ctx, *name, *email, strings.Split(*roles, ","))
	if err != nil {
		return err
	}
	// Printed once to the operator's terminal and never logged or stored in plain text.
	fmt.Printf("Created %s\nPassword:      %s\nTOTP secret:   %s\nAuthenticator: %s\n", *email, pw, secret, uri)
	return nil
}

//go:embed demo_seed.sql
var demoSeedSQL string

// seedDemo loads demo service areas, categories, services, rate card, vetting checklists and
// subscription plans so staging can be tried end to end. It only adds missing rows.
func seedDemo(ctx context.Context, cfg config.Config) error {
	if cfg.Env == "prod" {
		return errors.New("seed-demo is not allowed when APP_ENV=prod")
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, demoSeedSQL); err != nil {
		return fmt.Errorf("seed demo data: %w", err)
	}
	fmt.Println("Demo catalogue loaded (existing rows were left as they were).")
	return nil
}

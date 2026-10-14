// Package config reads service configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env             string // "dev" or "prod"
	HTTPAddr        string
	DatabaseURL     string
	CookieSecure    bool
	AllowedOrigins  []string
	SMSProvider     string // "log" prints codes to the log (dev); anything else is wired in sms.New
	ShutdownTimeout time.Duration
	FieldKey        string // base64 32-byte key for *_enc columns (KMS data key in prod)

	PublicBaseURL  string   // externally reachable origin, used to build M-Pesa callback URLs
	WebhookToken   string   // shared secret Safaricom and the SMS gateway send as ?token= on callbacks
	WebhookCIDRs   []string // optional source allow-list for webhooks (Safaricom egress ranges)
	MpesaMode      string   // fake | sandbox | production
	MpesaKey       string
	MpesaSecret    string
	MpesaShortCode string
	MpesaPassKey   string
	MpesaInitiator string
	MpesaSecurity  string
	MpesaB2CCode   string

	// Object storage for the document vault. "local" keeps files on disk and is for development only.
	StorageDriver string
	StorageDir    string
	StorageBucket string
	StorageKMSKey string
}

func Load() (Config, error) {
	c := Config{
		Env:             get("APP_ENV", "dev"),
		HTTPAddr:        get("HTTP_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		SMSProvider:     get("SMS_PROVIDER", "log"),
		ShutdownTimeout: 15 * time.Second,
		FieldKey:        os.Getenv("FIELD_ENCRYPTION_KEY"),
	}
	c.PublicBaseURL = get("PUBLIC_BASE_URL", "http://localhost:8080")
	c.WebhookToken = os.Getenv("WEBHOOK_TOKEN")
	if v := os.Getenv("WEBHOOK_ALLOWED_CIDRS"); v != "" {
		c.WebhookCIDRs = strings.Split(v, ",")
	}
	c.MpesaMode = get("MPESA_MODE", "fake")
	c.MpesaKey, c.MpesaSecret = os.Getenv("MPESA_CONSUMER_KEY"), os.Getenv("MPESA_CONSUMER_SECRET")
	c.MpesaShortCode, c.MpesaPassKey = os.Getenv("MPESA_SHORTCODE"), os.Getenv("MPESA_PASSKEY")
	c.MpesaInitiator, c.MpesaSecurity, c.MpesaB2CCode = os.Getenv("MPESA_INITIATOR"), os.Getenv("MPESA_SECURITY_CREDENTIAL"), get("MPESA_B2C_SHORTCODE", os.Getenv("MPESA_SHORTCODE"))
	c.StorageDriver = get("STORAGE_DRIVER", "local")
	c.StorageDir = get("STORAGE_DIR", os.TempDir()+"/hr-vault")
	c.StorageBucket = os.Getenv("STORAGE_BUCKET")
	c.StorageKMSKey = os.Getenv("STORAGE_KMS_KEY_ID")
	c.CookieSecure = getBool("COOKIE_SECURE", c.Env == "prod")
	if v := os.Getenv("ALLOWED_ORIGINS"); v != "" {
		c.AllowedOrigins = strings.Split(v, ",")
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if c.Env == "prod" && c.SMSProvider == "log" {
		return c, fmt.Errorf("SMS_PROVIDER=log is not allowed when APP_ENV=prod")
	}
	if c.Env == "prod" {
		if c.MpesaMode == "fake" {
			return c, fmt.Errorf("MPESA_MODE=fake is not allowed when APP_ENV=prod")
		}
		if c.WebhookToken == "" {
			return c, fmt.Errorf("WEBHOOK_TOKEN is required when APP_ENV=prod")
		}
		if c.StorageDriver == "local" {
			return c, fmt.Errorf("STORAGE_DRIVER=local is not allowed when APP_ENV=prod")
		}
	}
	if c.FieldKey == "" {
		if c.Env == "prod" {
			return c, fmt.Errorf("FIELD_ENCRYPTION_KEY is required when APP_ENV=prod")
		}
		// Fixed development key. Never used in prod (checked above).
		c.FieldKey = "ZGV2LW9ubHkta2V5LWRvLW5vdC11c2UtaW4tcHJvZCE="
	}
	return c, nil
}

func get(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

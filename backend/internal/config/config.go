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

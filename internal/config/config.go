package config

import (
	"errors"
	"os"
)

// Config holds all runtime configuration, loaded from environment variables.
type Config struct {
	DBDSN         string // e.g. sqlserver://user:pass@host:1433?database=name
	Addr          string // listen address, default :1112
	SessionSecret string // secret for signing session tokens
	AutoMigrate   bool   // run migrations on startup (true in dev, false in prod)
}

func Load() (*Config, error) {
	c := &Config{
		DBDSN:         os.Getenv("INFRACAP_DB_DSN"),
		Addr:          getenv("INFRACAP_ADDR", ":1112"),
		SessionSecret: os.Getenv("INFRACAP_SESSION_SECRET"),
		AutoMigrate:   os.Getenv("INFRACAP_AUTO_MIGRATE") == "true",
	}
	if c.DBDSN == "" {
		return nil, errors.New("INFRACAP_DB_DSN is required")
	}
	if c.SessionSecret == "" {
		return nil, errors.New("INFRACAP_SESSION_SECRET is required")
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

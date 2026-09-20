// Package app wires the service modes with Uber Fx.
//
// The binary runs a single mode per process (api, consumer or workers).
// Domain logic stays out of this package; it only composes configuration,
// servers, workers and their lifecycle.
package app

import (
	"errors"
	"fmt"
	"os"
)

// Mode selects which role the single binary plays in a process.
type Mode string

const (
	ModeAPI      Mode = "api"
	ModeConsumer Mode = "consumer"
	ModeWorkers  Mode = "workers"
)

// Config is the process configuration, loaded from the environment.
type Config struct {
	Mode        Mode
	HTTPAddr    string
	AdminAddr   string
	DatabaseURL string
	SQSEndpoint string
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// LoadFromEnv reads and validates process configuration.
// It fails fast on unknown modes or missing required values.
func LoadFromEnv() (Config, error) {
	mode := Mode(getenv("APP_MODE", "api"))
	switch mode {
	case ModeAPI, ModeConsumer, ModeWorkers:
	default:
		return Config{}, fmt.Errorf("invalid APP_MODE %q: must be api, consumer or workers", mode)
	}
	cfg := Config{
		Mode:        mode,
		HTTPAddr:    getenv("HTTP_ADDR", ":8080"),
		AdminAddr:   getenv("ADMIN_ADDR", ":9090"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		SQSEndpoint: getenv("SQS_ENDPOINT", "http://ministack:4566"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	return cfg, nil
}

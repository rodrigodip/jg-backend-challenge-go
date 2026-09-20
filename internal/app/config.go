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
	// OIDCIssuer is the expected token iss (deployment-facing Keycloak URL,
	// e.g. http://keycloak:8080/realms/wallet in-cluster). Required in api
	// mode: authentication cannot start without it.
	OIDCIssuer string
	// OIDCAudience optionally pins the expected aud. The wallet realm issues
	// no aud claim, so empty means "no audience check" (iss + signature +
	// roles + provider_id carry the verification).
	OIDCAudience string
	// Broker credentials and queues (5.3). Compose assigns one keypair per
	// role (consumer vs publisher); empty keys fall back to the SDK default
	// chain. Queue names default to the provisioned FIFO queues.
	AWSRegion      string
	AWSAccessKey   string
	AWSSecretKey   string
	SQSTxQueue     string
	SQSTxDLQ       string
	SQSEventsQueue string
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
		Mode:           mode,
		HTTPAddr:       getenv("HTTP_ADDR", ":8080"),
		AdminAddr:      getenv("ADMIN_ADDR", ":9090"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		SQSEndpoint:    getenv("SQS_ENDPOINT", "http://ministack:4566"),
		OIDCIssuer:     os.Getenv("OIDC_ISSUER"),
		OIDCAudience:   os.Getenv("OIDC_AUDIENCE"),
		AWSRegion:      getenv("AWS_REGION", "us-east-1"),
		AWSAccessKey:   os.Getenv("AWS_ACCESS_KEY_ID"),
		AWSSecretKey:   os.Getenv("AWS_SECRET_ACCESS_KEY"),
		SQSTxQueue:     getenv("SQS_TX_QUEUE", "wager-transactions.fifo"),
		SQSTxDLQ:       getenv("SQS_TX_DLQ", "wager-transactions-dlq.fifo"),
		SQSEventsQueue: getenv("SQS_EVENTS_QUEUE", "wager-events.fifo"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if mode == ModeAPI && cfg.OIDCIssuer == "" {
		return Config{}, errors.New("OIDC_ISSUER is required in api mode")
	}
	return cfg, nil
}

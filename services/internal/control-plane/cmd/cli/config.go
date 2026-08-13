package main

import (
	"errors"
	"time"

	"github.com/caarlos0/env/v11"
)

type migrationConfig struct {
	DSNFile                             string `env:"CONTROL_PLANE_POSTGRES_MIGRATION_DSN_FILE,required,notEmpty"`
	TLSServerName                       string `env:"CONTROL_PLANE_POSTGRES_TLS_SERVER_NAME,required,notEmpty"`
	CAFile                              string `env:"CONTROL_PLANE_POSTGRES_CA_FILE,required,notEmpty"`
	KeysetGenesisEnabled                bool   `env:"CONTROL_PLANE_KEYSET_GENESIS_ENABLED"`
	MattermostEventPublicKeysetFile     string `env:"CONTROL_PLANE_MATTERMOST_EVENT_PUBLIC_KEYSET_FILE"`
	InteractionReadbackPublicKeysetFile string `env:"CONTROL_PLANE_INTERACTION_READBACK_PUBLIC_KEYSET_FILE"`
}

type brokerConfig struct {
	URL             string        `env:"CONTROL_PLANE_NATS_URL,required,notEmpty"`
	TLSServerName   string        `env:"CONTROL_PLANE_NATS_TLS_SERVER_NAME,required,notEmpty"`
	CAFile          string        `env:"CONTROL_PLANE_NATS_CA_FILE,required,notEmpty"`
	CertificateFile string        `env:"CONTROL_PLANE_TLS_CERTIFICATE_FILE,required,notEmpty"`
	PrivateKeyFile  string        `env:"CONTROL_PLANE_TLS_PRIVATE_KEY_FILE,required,notEmpty"`
	CredentialsFile string        `env:"CONTROL_PLANE_NATS_CREDENTIALS_FILE,required,notEmpty"`
	Stream          string        `env:"CONTROL_PLANE_NATS_STREAM,required,notEmpty"`
	Replicas        int           `env:"CONTROL_PLANE_NATS_REPLICAS,required"`
	MaxBytes        int64         `env:"CONTROL_PLANE_NATS_MAX_BYTES,required"`
	ConnectTimeout  time.Duration `env:"CONTROL_PLANE_READINESS_TIMEOUT"`
}

type runtimePrincipalConfig struct {
	ContextKeyID       string `env:"CONTROL_PLANE_POSTGRES_CONTEXT_KEY_ID,required,notEmpty"`
	ContextKeyFile     string `env:"CONTROL_PLANE_POSTGRES_CONTEXT_KEY_FILE,required,notEmpty"`
	CurrentName        string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_CURRENT_PRINCIPAL,required,notEmpty"`
	CurrentGeneration  uint64 `env:"CONTROL_PLANE_POSTGRES_RUNTIME_CURRENT_GENERATION,required"`
	CurrentNotBefore   string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_CURRENT_NOT_BEFORE,required,notEmpty"`
	CurrentNotAfter    string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_CURRENT_NOT_AFTER,required,notEmpty"`
	CurrentDSNFile     string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_CURRENT_DSN_FILE,required,notEmpty"`
	NextName           string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_PRINCIPAL"`
	NextGeneration     uint64 `env:"CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_GENERATION"`
	NextNotBefore      string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_NOT_BEFORE"`
	NextNotAfter       string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_NOT_AFTER"`
	NextDSNFile        string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_NEXT_DSN_FILE"`
	PreviousName       string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_PREVIOUS_PRINCIPAL"`
	PreviousGeneration uint64 `env:"CONTROL_PLANE_POSTGRES_RUNTIME_PREVIOUS_GENERATION"`
	PreviousNotBefore  string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_PREVIOUS_NOT_BEFORE"`
	PreviousNotAfter   string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_PREVIOUS_NOT_AFTER"`
	PreviousDSNFile    string `env:"CONTROL_PLANE_POSTGRES_RUNTIME_PREVIOUS_DSN_FILE"`
}

type runtimePrincipal struct {
	PrincipalName string    `json:"principal_name"`
	Generation    uint64    `json:"generation"`
	Status        string    `json:"status"`
	NotBefore     time.Time `json:"not_before"`
	NotAfter      time.Time `json:"not_after"`
}

func loadMigrationConfig() (migrationConfig, error) {
	var config migrationConfig
	if err := env.Parse(&config); err != nil {
		return migrationConfig{}, errors.New("parse migration environment")
	}
	return config, nil
}

func loadBrokerConfig() (brokerConfig, error) {
	config := brokerConfig{
		ConnectTimeout: 2 * time.Second,
	}
	if err := env.Parse(&config); err != nil {
		return brokerConfig{}, errors.New("parse broker bootstrap environment")
	}
	return config, nil
}

func loadRuntimePrincipalConfig() (runtimePrincipalConfig, error) {
	var config runtimePrincipalConfig
	if err := env.Parse(&config); err != nil {
		return runtimePrincipalConfig{}, errors.New(
			"parse runtime principal reconciliation environment",
		)
	}
	return config, nil
}

func parseRuntimePrincipal(
	name string,
	generation uint64,
	status, notBeforeRaw, notAfterRaw string,
) (runtimePrincipal, error) {
	notBefore, beforeErr := time.Parse(time.RFC3339, notBeforeRaw)
	notAfter, afterErr := time.Parse(time.RFC3339, notAfterRaw)
	if name == "" || generation == 0 || beforeErr != nil || afterErr != nil ||
		!notAfter.After(notBefore) {
		return runtimePrincipal{}, errors.New(
			"runtime principal lifecycle input is invalid",
		)
	}
	return runtimePrincipal{
		PrincipalName: name,
		Generation:    generation,
		Status:        status,
		NotBefore:     notBefore.UTC(),
		NotAfter:      notAfter.UTC(),
	}, nil
}

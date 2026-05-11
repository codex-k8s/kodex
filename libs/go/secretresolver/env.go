package secretresolver

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// EnvBackend resolves secrets already injected into process environment variables.
type EnvBackend struct{}

// NewEnvBackend creates a backend for env refs where StoreRef is an env var name.
func NewEnvBackend() *EnvBackend {
	return &EnvBackend{}
}

// Resolve reads one environment variable into an in-memory SecretValue.
func (b *EnvBackend) Resolve(ctx context.Context, ref SecretRef) (SecretValue, error) {
	if err := ctx.Err(); err != nil {
		return SecretValue{}, err
	}
	name, err := parseEnvRef(ref)
	if err != nil {
		return SecretValue{}, err
	}
	value, ok := os.LookupEnv(name)
	if !ok {
		return SecretValue{}, ErrSecretNotFound
	}
	return newSecretValueFromString(value), nil
}

// Check verifies environment variable presence without returning the value.
func (b *EnvBackend) Check(ctx context.Context, ref SecretRef) (SecretStatus, error) {
	if err := ctx.Err(); err != nil {
		return SecretStatus{}, err
	}
	name, err := parseEnvRef(ref)
	if err != nil {
		return SecretStatus{}, err
	}
	if _, ok := os.LookupEnv(name); !ok {
		return SecretStatus{}, ErrSecretNotFound
	}
	return SecretStatus{Present: true, Version: "process-env"}, nil
}

func parseEnvRef(ref SecretRef) (string, error) {
	normalized, err := normalizeRef(ref)
	if err != nil {
		return "", err
	}
	if normalized.StoreType != StoreTypeEnv {
		return "", ErrUnsupportedStoreType
	}
	name := strings.TrimSpace(normalized.StoreRef)
	if !safeEnvName(name) {
		return "", fmt.Errorf("%w: invalid env secret ref", ErrInvalidRef)
	}
	return name, nil
}

func safeEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

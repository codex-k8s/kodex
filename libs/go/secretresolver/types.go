package secretresolver

import (
	"context"
	"errors"
	"strings"
)

const (
	// StoreTypeKubernetesSecret resolves references backed by Kubernetes Secret material.
	StoreTypeKubernetesSecret = "kubernetes_secret"
	// StoreTypeVault resolves references backed by Vault material.
	StoreTypeVault = "vault"
)

var (
	// ErrInvalidRef means a secret reference is malformed.
	ErrInvalidRef = errors.New("invalid secret reference")
	// ErrUnsupportedStoreType means no backend is registered for the store type.
	ErrUnsupportedStoreType = errors.New("unsupported secret store type")
	// ErrSecretNotFound means the backend did not find the referenced secret.
	ErrSecretNotFound = errors.New("secret not found")
	// ErrSecretUnavailable means the backend failed without exposing secret data.
	ErrSecretUnavailable = errors.New("secret unavailable")
	// ErrSecretSerialization means code attempted to serialize a secret value.
	ErrSecretSerialization = errors.New("secret value serialization is forbidden")
)

// SecretRef is the safe reference returned by access-manager after an access decision.
type SecretRef struct {
	StoreType string
	StoreRef  string
}

// SecretStatus is a value-free presence check result.
type SecretStatus struct {
	Present bool
	Version string
}

// Resolver returns a secret value for an already authorized operation.
type Resolver interface {
	Resolve(ctx context.Context, ref SecretRef) (SecretValue, error)
}

// Checker checks whether a secret reference is resolvable without returning the value.
type Checker interface {
	Check(ctx context.Context, ref SecretRef) (SecretStatus, error)
}

// Backend is implemented by concrete secret stores.
type Backend interface {
	Resolver
	Checker
}

func normalizeRef(ref SecretRef) (SecretRef, error) {
	normalized := SecretRef{
		StoreType: normalizeStoreType(ref.StoreType),
		StoreRef:  strings.TrimSpace(ref.StoreRef),
	}
	if normalized.StoreType == "" || normalized.StoreRef == "" {
		return SecretRef{}, ErrInvalidRef
	}
	return normalized, nil
}

func normalizeStoreType(storeType string) string {
	return strings.TrimSpace(storeType)
}

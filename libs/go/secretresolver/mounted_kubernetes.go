package secretresolver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultMaxSecretBytes int64 = 1 << 20

// MountedKubernetesBackendConfig configures a filesystem-backed Kubernetes Secret resolver.
type MountedKubernetesBackendConfig struct {
	Root           string
	MaxSecretBytes int64
}

// MountedKubernetesBackend reads Kubernetes Secret values from mounted files.
type MountedKubernetesBackend struct {
	root           string
	maxSecretBytes int64
}

type mountedKubernetesRef struct {
	namespace string
	name      string
	key       string
}

// NewMountedKubernetesBackend creates a backend for refs in namespace/name#key form.
func NewMountedKubernetesBackend(cfg MountedKubernetesBackendConfig) (*MountedKubernetesBackend, error) {
	root := strings.TrimSpace(cfg.Root)
	if root == "" {
		return nil, fmt.Errorf("%w: mounted Kubernetes Secret root is required", ErrInvalidRef)
	}
	maxSecretBytes := cfg.MaxSecretBytes
	if maxSecretBytes <= 0 {
		maxSecretBytes = defaultMaxSecretBytes
	}
	return &MountedKubernetesBackend{root: filepath.Clean(root), maxSecretBytes: maxSecretBytes}, nil
}

// Resolve reads one mounted secret file into an in-memory SecretValue.
func (b *MountedKubernetesBackend) Resolve(ctx context.Context, ref SecretRef) (SecretValue, error) {
	if err := ctx.Err(); err != nil {
		return SecretValue{}, err
	}
	path, err := b.path(ref)
	if err != nil {
		return SecretValue{}, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return SecretValue{}, ErrSecretNotFound
	}
	if err != nil {
		return SecretValue{}, fmt.Errorf("%w: mounted Kubernetes Secret read failed", ErrSecretUnavailable)
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, b.maxSecretBytes+1))
	if err != nil {
		clearBytes(raw)
		return SecretValue{}, fmt.Errorf("%w: mounted Kubernetes Secret read failed", ErrSecretUnavailable)
	}
	if int64(len(raw)) > b.maxSecretBytes {
		clearBytes(raw)
		return SecretValue{}, fmt.Errorf("%w: mounted Kubernetes Secret exceeds max size", ErrSecretUnavailable)
	}
	if err := ctx.Err(); err != nil {
		clearBytes(raw)
		return SecretValue{}, err
	}
	return newSecretValueOwned(raw), nil
}

// Check verifies file presence without reading the secret value.
func (b *MountedKubernetesBackend) Check(ctx context.Context, ref SecretRef) (SecretStatus, error) {
	if err := ctx.Err(); err != nil {
		return SecretStatus{}, err
	}
	path, err := b.path(ref)
	if err != nil {
		return SecretStatus{}, err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return SecretStatus{}, ErrSecretNotFound
	}
	if err != nil {
		return SecretStatus{}, fmt.Errorf("%w: mounted Kubernetes Secret check failed", ErrSecretUnavailable)
	}
	return SecretStatus{Present: true, Version: info.ModTime().UTC().Format(time.RFC3339Nano)}, nil
}

func (b *MountedKubernetesBackend) path(ref SecretRef) (string, error) {
	if b == nil || strings.TrimSpace(b.root) == "" {
		return "", fmt.Errorf("%w: mounted Kubernetes Secret backend is not configured", ErrInvalidRef)
	}
	normalized, err := normalizeRef(ref)
	if err != nil {
		return "", err
	}
	if normalized.StoreType != StoreTypeKubernetesMountedSecret {
		return "", ErrUnsupportedStoreType
	}
	parsed, err := parseMountedKubernetesRef(normalized.StoreRef)
	if err != nil {
		return "", err
	}
	path := filepath.Join(b.root, parsed.namespace, parsed.name, parsed.key)
	cleanRoot := filepath.Clean(b.root)
	cleanPath := filepath.Clean(path)
	relative, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return "", ErrInvalidRef
	}
	return cleanPath, nil
}

func parseMountedKubernetesRef(raw string) (mountedKubernetesRef, error) {
	namespace, rest, ok := strings.Cut(strings.TrimSpace(raw), "/")
	if !ok {
		return mountedKubernetesRef{}, ErrInvalidRef
	}
	name, key, ok := strings.Cut(rest, "#")
	if !ok {
		return mountedKubernetesRef{}, ErrInvalidRef
	}
	parsed := mountedKubernetesRef{namespace: namespace, name: name, key: key}
	if !safeKubernetesRefSegment(parsed.namespace) || !safeKubernetesRefSegment(parsed.name) || !safeKubernetesRefSegment(parsed.key) {
		return mountedKubernetesRef{}, ErrInvalidRef
	}
	return parsed, nil
}

func safeKubernetesRefSegment(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." {
		return false
	}
	return !strings.ContainsAny(value, `/\`+"\x00")
}

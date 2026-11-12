package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
)

// configureModelProvider loads one bounded immutable nonsecret projection before repository bootstrap.
// ConfigMap symlinks are expected; closed decoding, deployment ownership and exact digest bind the content.
// configureModelProvider читает bounded immutable nonsecret projection до repository bootstrap.
// ConfigMap symlinks допустимы; closed decoding, deployment ownership и digest связывают содержимое.
func configureModelProvider(repository *providercredential.Service, path string) error {
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return runtimecontract.ErrModelProviderConfiguration
	}
	file, err := os.Open(path)
	if err != nil {
		return errors.New("model provider configuration is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > runtimecontract.MaximumModelProviderCatalogBytes {
		return runtimecontract.ErrModelProviderConfiguration
	}
	raw, err := io.ReadAll(io.LimitReader(file, runtimecontract.MaximumModelProviderCatalogBytes+1))
	if err != nil {
		return errors.New("read model provider configuration")
	}
	provider, err := runtimecontract.DecodeModelProviderCatalog(raw)
	if err != nil {
		return err
	}
	return repository.ConfigureModelProviderCatalog(provider)
}

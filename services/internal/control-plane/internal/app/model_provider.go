package app

import (
	"errors"
	"io"
	"os"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	platformrepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
)

// configureModelProvider loads one bounded immutable nonsecret projection before repository bootstrap.
// ConfigMap symlinks are expected; closed decoding, deployment ownership and exact digest bind the content.
// configureModelProvider читает bounded immutable nonsecret projection до repository bootstrap.
// ConfigMap symlinks допустимы; closed decoding, deployment ownership и digest связывают содержимое.
func configureModelProvider(repository *platformrepository.Repository, config Config) error {
	if config.ModelProviderConfigurationFile == "" {
		return nil
	}
	file, err := os.Open(config.ModelProviderConfigurationFile)
	if err != nil {
		return errors.New("model provider configuration is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > runtimecontract.MaximumModelProviderConfigurationBytes {
		return runtimecontract.ErrModelProviderConfiguration
	}
	raw, err := io.ReadAll(io.LimitReader(file, runtimecontract.MaximumModelProviderConfigurationBytes+1))
	if err != nil {
		return errors.New("read model provider configuration")
	}
	provider, err := runtimecontract.DecodeModelProviderConfiguration(raw)
	if err != nil {
		return err
	}
	return repository.ConfigureModelProvider(provider, config.ModelProviderDefaultModel)
}

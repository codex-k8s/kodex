package app

import (
	"errors"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/controlplane"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/sshidentity"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// newSSHIdentityHealth composes only the optional fully configured purpose path; no host, key or writable material operation is contacted at construction.
// newSSHIdentityHealth связывает лишь optional fully configured purpose path; host, key либо writable material operation при construction не вызываются.
func newSSHIdentityHealth(config Config, owner *controlplane.Owner, store *kubernetes.Store) (*sshidentity.HealthService, error) {
	if config.SSHQualificationFile == "" && config.SSHQualificationSHA256 == "" {
		return nil, nil
	}
	if !config.sshIdentityUploadEnabled() || config.validateSSHIdentityUpload() != nil || owner == nil || store == nil {
		return nil, errors.New(sshUploadConfigurationInvalid)
	}
	operator, err := sshtarget.NewOperatorQualifications(config.SSHQualificationFile, config.SSHQualificationSHA256)
	if err != nil {
		return nil, errors.New(sshUploadConfigurationInvalid)
	}
	purpose, err := store.SSHIdentityStore()
	if err != nil {
		return nil, errors.New(sshUploadUnavailable)
	}
	return sshidentity.NewHealthService(owner, purpose, sshtarget.KeyPreparer{}, sshtarget.IdentityHealthProber{Operator: operator})
}

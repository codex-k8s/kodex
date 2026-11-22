package app

import (
	"errors"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/controlplane"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/sshidentity"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// newSSHIdentityExecution binds the optional finite purpose path to the existing protected owner and exact private store; target configuration never comes from a gateway claim.
// newSSHIdentityExecution связывает optional finite purpose path с existing protected owner и exact private store; target configuration не поступает из gateway claim.
func newSSHIdentityExecution(config Config, owner *controlplane.Owner, store *kubernetes.Store) (*sshidentity.ExecutionService, error) {
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
	return sshidentity.NewExecutionService(owner, purpose, sshtarget.KeyPreparer{}, sshtarget.IdentityExecutor{Operator: operator})
}

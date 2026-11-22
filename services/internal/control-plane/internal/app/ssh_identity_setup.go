package app

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	platformrepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
)

// validSSHIdentitySetupConfiguration keeps the optional purpose closed until both deployment-selected HTTPS coordinates are present; a partial configuration never falls back to another endpoint.
// validSSHIdentitySetupConfiguration оставляет optional purpose закрытым до наличия обоих deployment-selected HTTPS coordinates; partial configuration не переключается на другой endpoint.
func validSSHIdentitySetupConfiguration(config Config) bool {
	endpoint := value.SSHIdentityUploadEndpoint{Origin: config.SSHIdentityUploadOrigin, TLSServerName: config.SSHIdentityUploadTLSServerName}
	return endpoint.Origin == "" && endpoint.TLSServerName == "" || endpoint.Valid()
}

// configureSSHIdentitySetup binds trusted startup metadata before serving owner methods; private key bytes and caller configuration do not enter this composition hook.
// configureSSHIdentitySetup связывает trusted startup metadata до обслуживания owner methods; private key bytes и caller configuration не входят в composition hook.
func configureSSHIdentitySetup(repository *platformrepository.Repository, config Config) error {
	return repository.ConfigureSSHIdentitySetup(value.SSHIdentityUploadEndpoint{Origin: config.SSHIdentityUploadOrigin, TLSServerName: config.SSHIdentityUploadTLSServerName})
}

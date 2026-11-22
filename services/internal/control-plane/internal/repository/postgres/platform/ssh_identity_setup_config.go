package platform

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"

// ConfigureSSHIdentitySetup accepts only trusted deployment configuration before serving requests; an absent endpoint leaves the purpose unavailable and never falls back to TOKEN or another broker route.
// ConfigureSSHIdentitySetup принимает только trusted deployment configuration до обслуживания requests; отсутствие endpoint оставляет purpose недоступным без fallback к TOKEN либо другому broker route.
func (repository *Repository) ConfigureSSHIdentitySetup(endpoint value.SSHIdentityUploadEndpoint) error {
	if endpoint.Origin == "" && endpoint.TLSServerName == "" {
		repository.sshIdentityUploadEndpoint = nil
		return nil
	}
	if !endpoint.Valid() {
		return value.ErrSSHIdentityUploadEndpointInvalid
	}
	copy := endpoint
	repository.sshIdentityUploadEndpoint = &copy
	return nil
}

package app

import (
	"errors"
	"net/url"
	"path/filepath"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

const (
	// sshIdentityNamespace fixes the separately owned private-key namespace; it is not a caller-selected storage locator.
	// sshIdentityNamespace фиксирует separately owned private-key namespace, не caller-selected storage locator.
	sshIdentityNamespace = "kodex-ssh-identities"
	// sshUploadConfigurationInvalid is a closed configuration diagnostic without URLs, key paths or private data.
	// sshUploadConfigurationInvalid является closed configuration diagnostic без URLs, key paths либо private data.
	sshUploadConfigurationInvalid = "SSH identity upload configuration is invalid"
)

// sshIdentityUploadEnabled distinguishes explicitly configured custody from the default disabled feature; one partial field cannot select a fallback or another TLS listener.
// sshIdentityUploadEnabled отличает explicitly configured custody от default disabled feature; partial field не выбирает fallback либо другой TLS listener.
func (config Config) sshIdentityUploadEnabled() bool {
	return config.SSHIdentityNamespace != "" || config.SSHUploadListen != "" || config.SSHUploadOrigin != "" || config.SSHUploadBrowserOrigin != "" || config.SSHUploadCertificateFile != "" || config.SSHUploadPrivateKeyFile != ""
}

// validateSSHIdentityUpload requires the complete explicit protected route and exact purpose namespace; absence leaves the existing broker profile unchanged and cannot expose plaintext or generic TOKEN setup.
// validateSSHIdentityUpload требует полный explicit protected route и exact purpose namespace; отсутствие сохраняет existing broker profile без plaintext либо generic TOKEN setup.
func (config Config) validateSSHIdentityUpload() error {
	if !config.sshIdentityUploadEnabled() {
		return nil
	}
	origin, err := url.Parse(config.SSHUploadOrigin)
	if err != nil {
		return errors.New(sshUploadConfigurationInvalid)
	}
	endpoint := managedssh.IdentityUploadEndpoint{Origin: config.SSHUploadOrigin, TLSServerName: origin.Hostname()}
	browser, browserErr := url.Parse(config.SSHUploadBrowserOrigin)
	if config.SSHIdentityNamespace != sshIdentityNamespace || config.SSHUploadListen != ":8446" || !endpoint.Valid() || browserErr != nil || !(managedssh.IdentityUploadEndpoint{Origin: config.SSHUploadBrowserOrigin, TLSServerName: browser.Hostname()}).Valid() || !filepath.IsAbs(config.SSHUploadCertificateFile) || filepath.Clean(config.SSHUploadCertificateFile) != config.SSHUploadCertificateFile || !filepath.IsAbs(config.SSHUploadPrivateKeyFile) || filepath.Clean(config.SSHUploadPrivateKeyFile) != config.SSHUploadPrivateKeyFile || !managedssh.OwnerReference(config.ClaimantID) {
		return errors.New(sshUploadConfigurationInvalid)
	}
	return nil
}

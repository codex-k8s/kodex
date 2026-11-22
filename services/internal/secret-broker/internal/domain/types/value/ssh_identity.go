package value

import "github.com/codex-k8s/kodex/libs/go/managedssh"

// SSHIdentityPurpose pins the shared owner/broker material lane, preserving the separate SSH purpose and excluding generic TOKEN descriptors.
// SSHIdentityPurpose закрепляет общий owner/broker material lane, сохраняя отдельный SSH purpose и исключая generic TOKEN descriptors.
const SSHIdentityPurpose = managedssh.IdentityPurpose

// SSHIdentityLocator consumes the shared exact private material contract; no public credential view or gateway receives these physical pins.
// SSHIdentityLocator использует общий exact private material contract; public credential view и gateway не получают эти physical pins.
type SSHIdentityLocator = managedssh.IdentityLocator

// SSHIdentityProvision consumes the same immutable owner tuple as the private producer; lease/fence changes never change the material identity.
// SSHIdentityProvision использует тот же immutable owner tuple, что private producer; lease/fence changes не меняют material identity.
type SSHIdentityProvision = managedssh.IdentityProvision

// SSHIdentityKeyMetadata retains only actual broker-derived integrity hashes, without key bytes, credential policy or caller-assigned permissions.
// SSHIdentityKeyMetadata сохраняет только actual broker-derived integrity hashes без key bytes, credential policy либо caller-assigned permissions.
type SSHIdentityKeyMetadata = managedssh.IdentityKeyMetadata

// SSHIdentitySecretName preserves the existing private object naming contract through its shared producer/consumer predicate.
// SSHIdentitySecretName сохраняет existing private object naming contract через общий producer/consumer predicate.
func SSHIdentitySecretName(revisionRef string, revision int64) string {
	return managedssh.IdentitySecretName(revisionRef, revision)
}

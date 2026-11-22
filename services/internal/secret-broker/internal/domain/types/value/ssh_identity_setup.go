package value

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// SSHIdentityUploadMode is an owner-selected action, never a client request for another material write.
// SSHIdentityUploadMode задаёт owner-selected action, а не client request на другую material write.
type SSHIdentityUploadMode string

const (
	// SSHIdentityUploadWrite spends the setup's sole durable create intent before storage is called.
	// SSHIdentityUploadWrite расходует единственный durable create intent setup до вызова storage.
	SSHIdentityUploadWrite SSHIdentityUploadMode = "WRITE"
	// SSHIdentityUploadObserve reads only the original intent after a lost acknowledgement, without creating material or accepting a post-binding replacement.
	// SSHIdentityUploadObserve читает только original intent после lost acknowledgement без создания material либо принятия post-binding replacement.
	SSHIdentityUploadObserve SSHIdentityUploadMode = "OBSERVE"
)

// SSHIdentityPreparation contains owner-resolved immutable coordinates before the broker has parsed the key; the authentication digest is deliberately absent until Claim pins its actual derived value.
// SSHIdentityPreparation содержит owner-resolved immutable coordinates до разбора key broker; authentication digest намеренно отсутствует, пока Claim не закрепит actual derived value.
type SSHIdentityPreparation = managedssh.IdentityPreparation

// SSHIdentityUploadSnapshot is private owner state for exactly one setup; WRITE lacks a known key or locator, whereas OBSERVE retains the already immutable complete provision.
// SSHIdentityUploadSnapshot содержит private owner state одного setup; WRITE не имеет known key либо locator, тогда как OBSERVE сохраняет уже immutable complete provision.
type SSHIdentityUploadSnapshot struct {
	SetupRef, Digest     string
	Mode                 SSHIdentityUploadMode
	Preparation          SSHIdentityPreparation
	ContentBytes         uint32
	AuthenticationSHA256 string
	ExpiresAt            time.Time
}

// Valid rejects expired, malformed and mixed-phase snapshots before a key is parsed or a claim is requested; namespace selection still belongs to trusted broker composition.
// Valid отклоняет expired, malformed и mixed-phase snapshots до разбора key либо запроса claim; namespace selection остаётся trusted broker composition.
func (snapshot SSHIdentityUploadSnapshot) Valid(now time.Time) bool {
	if !managedssh.OwnerReference(snapshot.SetupRef) || !managedssh.Digest(snapshot.Digest) || snapshot.ContentBytes < 1 || snapshot.ContentBytes > 16384 || !snapshot.ExpiresAt.After(now) || snapshot.ExpiresAt.After(now.Add(90*time.Second)) {
		return false
	}
	if !snapshot.Preparation.Valid() {
		return false
	}
	switch snapshot.Mode {
	case SSHIdentityUploadWrite:
		if snapshot.AuthenticationSHA256 != "" {
			return false
		}
	case SSHIdentityUploadObserve:
		if !managedssh.Digest(snapshot.AuthenticationSHA256) {
			return false
		}
	default:
		return false
	}
	return true
}

// SSHIdentityUploadAdmission is one fenced owner grant; every coordinate and mode must match the preceding immutable snapshot before the adapter receives storage credit.
// SSHIdentityUploadAdmission содержит один fenced owner grant; все coordinates и mode должны совпасть с preceding immutable snapshot до выдачи adapter storage credit.
type SSHIdentityUploadAdmission struct {
	SetupRef, SnapshotDigest, AttemptRef, WorkloadInstance string
	Fence                                                  string `json:"-"`
	Generation                                             int64
	Mode                                                   SSHIdentityUploadMode
	Provision                                              SSHIdentityProvision `json:"-"`
	ExpiresAt                                              time.Time
}

// ValidFor binds a current short lease to the complete owner snapshot and parsed key, rejecting an expanded deadline, switched instance or another material tuple before the storage call.
// ValidFor связывает current short lease с complete owner snapshot и parsed key, отклоняя expanded deadline, switched instance либо иной material tuple до storage call.
func (admission SSHIdentityUploadAdmission) ValidFor(snapshot SSHIdentityUploadSnapshot, key SSHIdentityKeyMetadata, instance string, now time.Time) bool {
	provision, valid := snapshot.Preparation.Provision(key)
	return valid && (snapshot.Mode != SSHIdentityUploadObserve || key.AuthenticationSHA256 == snapshot.AuthenticationSHA256) && snapshot.Valid(now) && managedssh.OwnerReference(instance) && admission.SetupRef == snapshot.SetupRef && admission.SnapshotDigest == snapshot.Digest && admission.Mode == snapshot.Mode &&
		admission.Provision == provision && managedssh.OwnerReference(admission.AttemptRef) && managedssh.OwnerReference(admission.Fence) && admission.WorkloadInstance == instance && admission.Generation >= 1 && admission.Generation <= 8 &&
		admission.ExpiresAt.After(now) && !admission.ExpiresAt.After(snapshot.ExpiresAt) && !admission.ExpiresAt.After(now.Add(30*time.Second))
}

// String hides the private grant fence and material tuple from ordinary formatting.
// String скрывает private grant fence и material tuple из ordinary formatting.
func (SSHIdentityUploadAdmission) String() string { return "[SSH private identity upload admission]" }

// GoString preserves the same private boundary for detailed Go formatting.
// GoString сохраняет ту же private boundary при подробном Go formatting.
func (admission SSHIdentityUploadAdmission) GoString() string { return admission.String() }

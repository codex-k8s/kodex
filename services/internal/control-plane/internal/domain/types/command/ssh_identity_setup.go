package command

import "github.com/codex-k8s/kodex/libs/go/managedssh"

// SSHIdentitySetupBegin supplies key-content preconditions and Connection OCC only; actor, credential policy, binding, target, physical namespace and identity revision are owner-resolved.
// SSHIdentitySetupBegin содержит лишь key-content preconditions и Connection OCC; actor, credential policy, binding, target, physical namespace и identity revision разрешает owner.
type SSHIdentitySetupBegin struct {
	ConnectionRef, ContentSHA256 string
	ExpectedConnectionVersion    int64
	ContentBytes                 uint32
}

// SSHIdentitySetupRead chooses one addressed setup or the original Begin receipt without recovering a consumed raw upload permit.
// SSHIdentitySetupRead выбирает addressed setup либо original Begin receipt без восстановления consumed raw upload permit.
type SSHIdentitySetupRead struct{ ConnectionRef, SetupRef, BeginIdempotencyKey string }

// SSHIdentitySetupCancel addresses one current owner setup; ExpectedVersion and idempotency are separate mutation inputs and do not establish ownership.
// SSHIdentitySetupCancel адресует current owner setup; ExpectedVersion и idempotency передаются отдельно и не устанавливают ownership.
type SSHIdentitySetupCancel struct{ ConnectionRef, SetupRef string }

// SSHIdentityUploadResolve carries a setup locator and hash of a one-shot permit; no key bytes, caller actor or alternate target may enter the owner request.
// SSHIdentityUploadResolve содержит setup locator и hash one-shot permit; key bytes, caller actor и alternate target не входят в owner request.
type SSHIdentityUploadResolve struct{ SetupRef, PermitSHA256 string }

// SSHIdentityUploadClaim binds the owner's immutable snapshot to actual metadata derived by the protected broker parser; current state alone chooses WRITE versus OBSERVE.
// SSHIdentityUploadClaim связывает immutable owner snapshot с actual metadata защищённого broker parser; только current state выбирает WRITE либо OBSERVE.
type SSHIdentityUploadClaim struct {
	SetupRef, PermitSHA256, SnapshotDigest, WorkloadInstance string
	Key                                                      managedssh.IdentityKeyMetadata
}

// SSHIdentityRejectedAcknowledgement contains only a bounded private successful-response witness, retaining an unambiguous observed UID even when other acknowledged fields failed validation.
// SSHIdentityRejectedAcknowledgement содержит лишь bounded private successful-response witness и сохраняет unambiguous observed UID даже при отказе validation остальных acknowledged fields.
type SSHIdentityRejectedAcknowledgement struct {
	ResponseSHA256 string
	Truncated      bool
	ObservedUID    string
}

// SSHIdentityUploadComplete reports exact pins, explicit transport ambiguity or a terminal damaged success; absence of a completion is a separate closed state and never becomes observation credit by timeout.
// SSHIdentityUploadComplete передаёт exact pins, explicit transport ambiguity либо terminal damaged success; missing completion является отдельным closed state и не становится observation credit по timeout.
type SSHIdentityUploadComplete struct {
	SetupRef, AttemptRef, WorkloadInstance string
	Fence                                  string `json:"-"`
	Generation                             int64
	Identity                               *managedssh.IdentityLocator `json:"-"`
	Unknown                                bool
	Rejected                               *SSHIdentityRejectedAcknowledgement `json:"-"`
}

// String conceals fences, private locators and acknowledgement witnesses even when a whole command is accidentally formatted.
// String скрывает fences, private locators и acknowledgement witnesses даже при accidental formatting всей command.
func (SSHIdentityUploadComplete) String() string { return "[SSH private identity upload completion]" }

// GoString preserves the same privacy boundary for detailed Go formatting.
// GoString сохраняет ту же privacy boundary при подробном Go formatting.
func (input SSHIdentityUploadComplete) GoString() string { return input.String() }

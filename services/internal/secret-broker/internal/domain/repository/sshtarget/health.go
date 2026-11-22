package sshtarget

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// IdentityHealthOwner consumes only existing owner claims and records joined read facts; it never receives private key bytes.
// IdentityHealthOwner расходует лишь existing owner claims и сохраняет joined read facts без private key bytes.
type IdentityHealthOwner interface {
	// BeginSSHIdentityHealthProbe admits one original health attempt before key access or SSH dispatch.
	// BeginSSHIdentityHealthProbe допускает original health attempt до key access либо SSH dispatch.
	BeginSSHIdentityHealthProbe(context.Context, managedssh.Binding) (managedssh.HealthAdmission, error)
	// CompleteSSHIdentityHealthProbe persists the original admission's joined read fact or unavailable observation, without retrying a probe.
	// CompleteSSHIdentityHealthProbe сохраняет joined read fact либо unavailable observation original admission без повторного probe.
	CompleteSSHIdentityHealthProbe(context.Context, managedssh.Binding, managedssh.HealthAdmission, managedssh.HealthObservation) (managedssh.HealthReceipt, error)
}

// IdentityHealthProber performs only fixed reads through operator-selected endpoints and a broker-held key; an error cannot be classified as credential denial here.
// IdentityHealthProber выполняет лишь fixed reads через operator-selected endpoints и broker-held key; error здесь не классифицируется как credential denial.
type IdentityHealthProber interface {
	// ProbeSSHIdentity joins the actual transport and returns no command output or credential bytes.
	// ProbeSSHIdentity завершает actual transport и не возвращает command output либо credential bytes.
	ProbeSSHIdentity(context.Context, managedssh.HealthAdmission, []byte) (managedssh.HealthObservation, error)
}

package sshtarget

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"time"
)

// HealthObservation retains joined authentication and exact operator evidence only; it carries no resource permission, stdout or credential material.
// HealthObservation сохраняет лишь joined authentication и exact operator evidence без resource permission, stdout либо credential material.
type HealthObservation = managedssh.HealthObservation

// ProbeIdentity resolves the endpoint and exact public keys only from the trusted current operator artifact, then performs fixed bounded reads with the owner-selected identity.
// ProbeIdentity разрешает endpoint и exact public keys лишь из trusted current operator artifact и выполняет fixed bounded reads с owner-selected identity.
func (client Client) ProbeIdentity(ctx context.Context, operator OperatorQualifications, targetRef string, privateKey []byte, mode managedssh.TransportMode, attemptRef string) (HealthObservation, error) {
	target, profile, err := operator.ResolveTarget(targetRef, privateKey, mode)
	if err != nil {
		return HealthObservation{}, err
	}
	refresh := func() error { _, err := operator.Resolve(targetRef, target, privateKey, mode); return err }
	return client.probeIdentity(ctx, target, privateKey, profile, attemptRef, refresh)
}

// probeIdentity reuses the production SSH transport and finite helper codec; execute health permits only fixed numeric identity reads and never accepts a caller command.
// probeIdentity использует production SSH transport и finite helper codec; execute health допускает лишь fixed numeric identity reads без caller command.
func (client Client) probeIdentity(ctx context.Context, target Target, privateKey []byte, profile managedssh.TargetQualification, attemptRef string, refresh func() error) (HealthObservation, error) {
	if ctx == nil || ctx.Err() != nil || profile.Validate(time.Now().Unix()) != nil || !managedssh.OwnerReference(attemptRef) || refresh == nil {
		return HealthObservation{}, ErrRejected
	}
	// The attempt name is only codec correlation for this read; owner authority remains the original health admission.
	// Attempt name служит лишь codec correlation этого read; owner authority остаётся original health admission.
	attempt, err := managedssh.HelperAttemptID(attemptRef)
	if err != nil {
		return HealthObservation{}, ErrRejected
	}
	switch profile.Mode {
	case managedssh.ManagedTransport:
		if profile.Profile == nil {
			return HealthObservation{}, ErrRejected
		}
		hash, err := profile.Profile.Digest()
		if err != nil {
			return HealthObservation{}, ErrRejected
		}
		request := ManagedRequest{Wire: managedssh.Request{Version: managedssh.Version, ProfileDigest: hash, AttemptID: attempt, Operation: managedssh.Inspect}}
		reply, err := client.managed(ctx, target, privateKey, *profile.Profile, request, refresh)
		if err != nil {
			return HealthObservation{}, err
		}
		if reply.Status != managedssh.Observed || reply.Identity == nil {
			return HealthObservation{}, ErrRejected
		}
	case managedssh.ExecuteTransport:
		result, err := client.run(ctx, target, privateKey, Command{Text: numericUIDCommand, MaximumOutput: 1024}, &profile, refresh)
		if err != nil {
			return HealthObservation{}, err
		}
		ids, err := numericIdentityValues(result.Stdout)
		if err != nil || !result.SessionExited || result.ExitStatus != 0 || len(result.Stderr) != 0 || len(ids) != 1 || ids[0] != profile.UID {
			return HealthObservation{}, ErrRejected
		}
	default:
		return HealthObservation{}, ErrRejected
	}
	if ctx.Err() != nil || profile.ValidUntil <= time.Now().Unix() || refresh() != nil {
		return HealthObservation{}, ErrRejected
	}
	digest, err := profile.Digest()
	if err != nil {
		return HealthObservation{}, ErrRejected
	}
	return HealthObservation{ObservedSubjectDigest: digest, QualifiedUntil: time.Unix(profile.ValidUntil, 0).UTC()}, nil
}

// IdentityHealthProber composes the existing transport with deployment-pinned qualification; product configuration cannot replace the operator artifact.
// IdentityHealthProber связывает existing transport с deployment-pinned qualification; product configuration не подменяет operator artifact.
type IdentityHealthProber struct {
	Client   Client
	Operator OperatorQualifications
}

// ProbeSSHIdentity uses owner-selected identity/target/mode and one exact admitted attempt without exposing private bytes to another layer.
// ProbeSSHIdentity использует owner-selected identity/target/mode и exact admitted attempt без выдачи private bytes другому слою.
func (prober IdentityHealthProber) ProbeSSHIdentity(ctx context.Context, admission managedssh.HealthAdmission, privateKey []byte) (managedssh.HealthObservation, error) {
	if !admission.Identity.Valid() || !managedssh.Digest(admission.SnapshotDigest) || !admission.ExpiresAt.After(time.Now()) {
		return managedssh.HealthObservation{}, ErrRejected
	}
	return prober.Client.ProbeIdentity(ctx, prober.Operator, admission.Identity.TargetRef, privateKey, admission.Mode, admission.AttemptRef)
}

package sshtarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// IdentityExecutor uses the existing one-connection SSH transport and immutable operator artifact; it contains no claim loop, retry policy or credential selector.
// IdentityExecutor использует existing one-connection SSH transport и immutable operator artifact без claim loop, retry policy либо credential selector.
type IdentityExecutor struct {
	Client   Client
	Operator OperatorQualifications
}

// ExecuteSSHIdentity resolves the actual endpoint from trusted deployment metadata and matches the complete profile to the owner's recent health observation before dialing.
// ExecuteSSHIdentity разрешает actual endpoint из trusted deployment metadata и сверяет full profile с недавним health observation owner до подключения.
func (executor IdentityExecutor) ExecuteSSHIdentity(ctx context.Context, admission managedssh.ExecutionAdmission, privateKey []byte, check func(context.Context) error) (managedssh.ExecutionObservation, error) {
	if ctx == nil || ctx.Err() != nil || admission.Validate(time.Now()) != nil || check == nil {
		return managedssh.ExecutionObservation{}, ErrRejected
	}
	if admission.Recover && admission.Mode == managedssh.ExecuteTransport {
		return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}, nil
	}
	target, profile, err := executor.Operator.ResolveTarget(admission.Identity.TargetRef, privateKey, admission.Mode)
	if err != nil {
		return managedssh.ExecutionObservation{}, err
	}
	operation, cancel := context.WithDeadline(ctx, admission.ExpiresAt)
	defer cancel()
	var authorityErr error
	refresh := func() error {
		if err := check(operation); err != nil {
			authorityErr = err
			return err
		}
		current, err := executor.Operator.Resolve(admission.Identity.TargetRef, target, privateKey, admission.Mode)
		if err != nil {
			return err
		}
		digest, err := current.Digest()
		if err != nil || digest != admission.SubjectDigest {
			return ErrRejected
		}
		return nil
	}
	observation, err := executor.Client.executeIdentity(operation, admission, target, profile, privateKey, refresh)
	if authorityErr != nil {
		clear(observation.Result)
		return managedssh.ExecutionObservation{}, authorityErr
	}
	return observation, err
}

// executeIdentity performs one finite recipe or separately EACH-admitted arbitrary execution; recovery sends only ReceiptRead, and arbitrary execution recovery never dials or resends a command.
// executeIdentity выполняет один finite recipe либо отдельно EACH-admitted arbitrary execution; recovery отправляет лишь ReceiptRead, а arbitrary execution recovery не подключается и не повторяет command.
func (client Client) executeIdentity(ctx context.Context, admission managedssh.ExecutionAdmission, target Target, profile managedssh.TargetQualification, privateKey []byte, refresh func() error) (managedssh.ExecutionObservation, error) {
	if ctx == nil || ctx.Err() != nil || admission.Validate(time.Now()) != nil || profile.Validate(time.Now().Unix()) != nil || profile.Mode != admission.Mode || profile.TargetRef != admission.Identity.TargetRef || refresh == nil {
		return managedssh.ExecutionObservation{}, ErrRejected
	}
	digest, err := profile.Digest()
	if err != nil || digest != admission.SubjectDigest || refresh() != nil {
		return managedssh.ExecutionObservation{}, ErrRejected
	}
	input, err := managedssh.DecodeOperationInput(admission.Operation, admission.CanonicalInput)
	if err != nil {
		return managedssh.ExecutionObservation{}, ErrRejected
	}
	defer input.Clear()
	if admission.Recover && input.Kind() == managedssh.ExecuteInput {
		return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}, nil
	}
	operation, cancel := context.WithDeadline(ctx, admission.ExpiresAt)
	defer cancel()
	var raw []byte
	outcome := managedssh.ExecutionSucceeded
	switch input.Kind() {
	case managedssh.ManagedInput, managedssh.ReceiptInput:
		if profile.Profile == nil {
			return managedssh.ExecutionObservation{}, ErrRejected
		}
		request, err := sshExecutionManagedRequest(admission, input, *profile.Profile)
		if err != nil {
			return managedssh.ExecutionObservation{}, err
		}
		defer clear(request.Wire.Content)
		reply, err := client.managed(operation, target, privateKey, *profile.Profile, request, refresh)
		if err != nil {
			if admission.Recover {
				return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}, nil
			}
			return sshExecutionFailure(err)
		}
		switch reply.Status {
		case managedssh.Unknown:
			return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}, nil
		case managedssh.Rejected:
			outcome = managedssh.ExecutionFailed
		case managedssh.Observed, managedssh.Applied:
		default:
			return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}, nil
		}
		if admission.Recover {
			if reply.Receipt == nil || reply.Receipt.Status == managedssh.Unknown {
				return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}, nil
			}
			if reply.Receipt.Status == managedssh.Rejected {
				outcome = managedssh.ExecutionFailed
			}
		}
		raw, err = managedssh.EncodeReply(request.Wire, reply)
		if err != nil {
			return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}, nil
		}
	case managedssh.ExecuteInput:
		text, stdin, maximum, err := input.Execution()
		if err != nil {
			return managedssh.ExecutionObservation{}, ErrRejected
		}
		defer clear(stdin)
		result, err := client.run(operation, target, privateKey, Command{Text: text, Input: stdin, MaximumOutput: int(maximum)}, &profile, refresh)
		defer clear(result.Stdout)
		defer clear(result.Stderr)
		if err != nil {
			return sshExecutionFailure(err)
		}
		if !result.SessionExited || result.Truncated {
			return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}, nil
		}
		if result.ExitStatus != 0 {
			outcome = managedssh.ExecutionFailed
		}
		raw, err = json.Marshal(struct {
			Stdout     []byte `json:"stdout,omitempty"`
			Stderr     []byte `json:"stderr,omitempty"`
			ExitStatus int    `json:"exit_status"`
		}{result.Stdout, result.Stderr, result.ExitStatus})
		if err != nil {
			return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}, nil
		}
	case managedssh.IdentityInput:
		observation, err := client.probeIdentity(operation, target, privateKey, profile, admission.AttemptRef, refresh)
		if err != nil {
			return sshExecutionFailure(err)
		}
		raw, err = json.Marshal(observation)
		if err != nil {
			return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}, nil
		}
	default:
		return managedssh.ExecutionObservation{}, ErrRejected
	}
	if operation.Err() != nil || len(raw) > 64<<10 {
		clear(raw)
		return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}, nil
	}
	resultDigest := sha256.Sum256(raw)
	return managedssh.ExecutionObservation{Outcome: outcome, Result: raw, ResultDigest: hex.EncodeToString(resultDigest[:])}, nil
}

// sshExecutionManagedRequest preserves the original owner invocation UUID and exact helper request digest through read-only reconciliation; no recovery path recreates the original operation.
// sshExecutionManagedRequest сохраняет original owner invocation UUID и exact helper request digest при read-only reconciliation; recovery не создаёт original operation заново.
func sshExecutionManagedRequest(admission managedssh.ExecutionAdmission, input managedssh.OperationInput, profile managedssh.Profile) (ManagedRequest, error) {
	profileDigest, err := profile.Digest()
	if err != nil {
		return ManagedRequest{}, ErrRejected
	}
	invocation := admission.InvocationRef
	if input.Kind() == managedssh.ReceiptInput {
		if admission.ReceiptSource == nil {
			return ManagedRequest{}, ErrRejected
		}
		invocation = admission.ReceiptSource.InvocationRef
		original, err := managedssh.DecodeOperationInput(admission.ReceiptSource.Operation, admission.ReceiptSource.CanonicalInput)
		if err != nil {
			return ManagedRequest{}, ErrRejected
		}
		defer original.Clear()
		input = original
	}
	attempt, err := managedssh.HelperAttemptID(invocation)
	if err != nil {
		return ManagedRequest{}, ErrRejected
	}
	original, err := input.Request(attempt, profileDigest)
	if err != nil {
		return ManagedRequest{}, ErrRejected
	}
	if !admission.Recover && admission.ReceiptSource == nil {
		return ManagedRequest{Wire: original}, nil
	}
	defer clear(original.Content)
	digest, err := managedssh.RequestDigest(original)
	if err != nil {
		return ManagedRequest{}, ErrRejected
	}
	return ManagedRequest{Wire: managedssh.Request{Version: managedssh.Version, ProfileDigest: profileDigest, AttemptID: attempt, Operation: managedssh.ReceiptRead}, Recovery: &RecoveryExpectation{Operation: original.Operation, Digest: digest}}, nil
}

// sshExecutionFailure preserves ambiguity over the private SDK cause, returning no partial output and never treating a failed status read as evidence that an earlier change failed.
// sshExecutionFailure сохраняет ambiguity поверх private SDK cause без partial output и не превращает failed status read в доказательство отказа прежнего изменения.
func sshExecutionFailure(err error) (managedssh.ExecutionObservation, error) {
	if errors.Is(err, ErrUnknown) {
		return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionUnknown}, nil
	}
	return managedssh.ExecutionObservation{Outcome: managedssh.ExecutionFailed}, nil
}

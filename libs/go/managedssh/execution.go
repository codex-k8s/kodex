package managedssh

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// ExecutionAdmission contains a one-attempt owner receipt and private exact-source pins, never a reusable grant or caller-selected endpoint; CanonicalInput remains owned sensitive data.
// ExecutionAdmission содержит one-attempt owner receipt и private exact-source pins без reusable grant либо caller-selected endpoint; CanonicalInput остаётся owned sensitive data.
type ExecutionAdmission struct {
	AttemptRef, InvocationRef, SnapshotDigest, Operation, InputDigest, SubjectDigest string
	Identity                                                                         IdentityLocator
	Mode                                                                             TransportMode
	CanonicalInput                                                                   []byte
	ExpiresAt                                                                        time.Time
	Recover                                                                          bool
	ReceiptSource                                                                    *ManagedReceiptSource
}

// ManagedReceiptSource is resolved by the owner from an immutable original invocation under the same custody and target; a caller's receipt reference cannot supply these pins.
// ManagedReceiptSource разрешает owner из immutable original invocation в тех же custody и target; caller receipt reference не задаёт эти pins.
type ManagedReceiptSource struct {
	InvocationRef, Operation string
	CanonicalInput           []byte
}

// Validate checks bounded private receipt structure and exact canonical input before any key read or SSH connection; current authority still belongs to the owner's transaction.
// Validate проверяет bounded private receipt structure и exact canonical input до key read либо SSH connection; current authority остаётся у owner transaction.
func (admission ExecutionAdmission) Validate(now time.Time) error {
	if !OwnerReference(admission.AttemptRef) || !OwnerReference(admission.InvocationRef) || !Digest(admission.SnapshotDigest) || !Digest(admission.InputDigest) || !Digest(admission.SubjectDigest) ||
		!admission.Identity.Valid() || (admission.Mode != ManagedTransport && admission.Mode != ExecuteTransport) || now.IsZero() ||
		!admission.ExpiresAt.After(now) || admission.ExpiresAt.After(now.Add(30*time.Second)) {
		return ErrProtocol
	}
	input, err := DecodeOperationInput(admission.Operation, admission.CanonicalInput)
	if err != nil {
		return ErrProtocol
	}
	defer input.Clear()
	digest, err := input.Digest()
	if err != nil || digest != admission.InputDigest || admission.Mode == ExecuteTransport && input.Kind() != ExecuteInput && input.Kind() != IdentityInput ||
		admission.Mode == ManagedTransport && input.Kind() != ManagedInput && input.Kind() != ReceiptInput || admission.Recover && input.Kind() != ManagedInput && input.Kind() != ExecuteInput {
		return ErrProtocol
	}
	if input.Kind() == ReceiptInput {
		ref, _ := input.ReceiptReference()
		if admission.ReceiptSource == nil || admission.ReceiptSource.InvocationRef != ref {
			return ErrProtocol
		}
		original, err := DecodeOperationInput(admission.ReceiptSource.Operation, admission.ReceiptSource.CanonicalInput)
		if err != nil {
			return ErrProtocol
		}
		defer original.Clear()
		effect, err := original.Effect()
		if !OwnerReference(ref) || original.Kind() != ManagedInput || err != nil || effect == ReadOnly {
			return ErrProtocol
		}
	} else if admission.ReceiptSource != nil {
		return ErrProtocol
	}
	if admission.Recover {
		effect, err := input.Effect()
		if err != nil || effect == ReadOnly {
			return ErrProtocol
		}
	}
	return nil
}

// String hides command bytes, locators and attempt bindings from ordinary diagnostic formatting.
// String скрывает command bytes, locators и attempt bindings при ordinary diagnostic formatting.
func (ExecutionAdmission) String() string { return "[SSH private execution admission]" }

// GoString retains the same privacy boundary under detailed formatting.
// GoString сохраняет ту же privacy boundary при detailed formatting.
func (admission ExecutionAdmission) GoString() string { return admission.String() }

// ExecutionOutcome separates a joined observed result, a known refusal and unresolved dispatch; UNKNOWN never permits resending the original command.
// ExecutionOutcome разделяет joined observed result, known refusal и unresolved dispatch; UNKNOWN не разрешает повтор original command.
type ExecutionOutcome string

const (
	// ExecutionSucceeded records a validated joined response, not remote descendant quiescence or credential authority.
	// ExecutionSucceeded фиксирует validated joined response, не remote descendant quiescence либо credential authority.
	ExecutionSucceeded ExecutionOutcome = "SUCCEEDED"
	// ExecutionFailed records a known refusal or joined nonzero command exit without guessing whether the user's intended effect succeeded.
	// ExecutionFailed фиксирует known refusal либо joined nonzero command exit без догадки об успехе предполагаемого user effect.
	ExecutionFailed ExecutionOutcome = "FAILED"
	// ExecutionUnknown retains an ambiguous effect and withholds output; reconciliation may only read the original managed receipt.
	// ExecutionUnknown сохраняет ambiguous effect без выдачи output; reconciliation может лишь читать original managed receipt.
	ExecutionUnknown ExecutionOutcome = "UNKNOWN"
)

// ExecutionObservation carries only bounded joined output or an explicit unresolved outcome; native transport errors and key material never enter this owner fact.
// ExecutionObservation содержит лишь bounded joined output либо explicit unresolved outcome; native transport errors и key material не входят в owner fact.
type ExecutionObservation struct {
	Outcome      ExecutionOutcome
	Result       []byte
	ResultDigest string
}

// Validate rejects mixed outcomes, partial UNKNOWN output and altered result bytes before the authoritative owner considers publication; a valid shape is not execution authority.
// Validate отклоняет mixed outcomes, partial UNKNOWN output и изменённые result bytes до рассмотрения publication authoritative owner; valid shape не является execution authority.
func (observation ExecutionObservation) Validate() error {
	if observation.Outcome != ExecutionSucceeded && observation.Outcome != ExecutionFailed && observation.Outcome != ExecutionUnknown {
		return ErrProtocol
	}
	if len(observation.Result) == 0 {
		if observation.Outcome == ExecutionSucceeded || observation.ResultDigest != "" {
			return ErrProtocol
		}
		return nil
	}
	if observation.Outcome == ExecutionUnknown || len(observation.Result) > 64<<10 || !Digest(observation.ResultDigest) || !json.Valid(observation.Result) || observation.Result[0] != '{' {
		return ErrProtocol
	}
	digest := sha256.Sum256(observation.Result)
	if hex.EncodeToString(digest[:]) != observation.ResultDigest {
		return ErrProtocol
	}
	return nil
}

// String conceals authorized but private output, which may be projected only through the exact owner result boundary.
// String скрывает authorized, но private output, выдаваемый лишь через exact owner result boundary.
func (ExecutionObservation) String() string { return "[SSH private execution observation]" }

// GoString retains redaction for detailed diagnostic formatting.
// GoString сохраняет redaction при detailed diagnostic formatting.
func (observation ExecutionObservation) GoString() string { return observation.String() }

// String conceals the original private input retained solely for owner-authorized receipt reconciliation.
// String скрывает original private input, сохранённый лишь для owner-authorized receipt reconciliation.
func (ManagedReceiptSource) String() string { return "[SSH private receipt source]" }

// GoString preserves the private input boundary under detailed formatting.
// GoString сохраняет private input boundary при detailed formatting.
func (source ManagedReceiptSource) GoString() string { return source.String() }

// HelperAttemptID derives one stable helper UUID from an owner-created original invocation reference; new leases cannot rename a durable managed effect.
// HelperAttemptID выводит стабильный helper UUID из owner-created original invocation reference; new leases не переименовывают durable managed effect.
func HelperAttemptID(invocationRef string) (string, error) {
	if !OwnerReference(invocationRef) {
		return "", ErrProtocol
	}
	digest := sha256.Sum256([]byte(invocationRef))
	digest[6], digest[8] = digest[6]&15|64, digest[8]&63|128
	return fmt.Sprintf("%x-%x-%x-%x-%x", digest[:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16]), nil
}

// Digest fixes the complete public operator qualification, including exact host/authentication pins, numeric identity, installed helper and expiry; it confers no authority by itself.
// Digest закрепляет полную public operator qualification, включая exact host/authentication pins, numeric identity, installed helper и expiry; сам digest не выдаёт authority.
func (qualification TargetQualification) Digest() (string, error) {
	raw, err := json.Marshal(qualification)
	if err != nil {
		return "", ErrProtocol
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// ExecutionReceipt retains the original owner intent and immutable publication decision; metadata reads never reissue admission or publish private result bytes.
// ExecutionReceipt сохраняет original owner intent и immutable publication decision; metadata reads не выдают повторную admission либо private result bytes.
type ExecutionReceipt struct {
	AttemptRef, SnapshotDigest, State, OwnerState string
	Recorded, Applied                             bool
	Observation                                   ExecutionObservation
	AuthorizedUntil                               time.Time
}

// Validate checks only the closed receipt shape, not present owner authority; metadata responses may carry a result digest without its private bytes.
// Validate проверяет лишь closed receipt shape, не present owner authority; metadata responses могут содержать result digest без private bytes.
func (receipt ExecutionReceipt) Validate(now time.Time, withResult bool) error {
	if !OwnerReference(receipt.AttemptRef) || !Digest(receipt.SnapshotDigest) {
		return ErrProtocol
	}
	switch receipt.OwnerState {
	case "READY", "WAITING_APPROVAL", "RUNNING", "SUCCEEDED", "FAILED", "CANCELLED", "UNKNOWN_OUTCOME":
	default:
		return ErrProtocol
	}
	if !receipt.AuthorizedUntil.IsZero() && receipt.OwnerState != "RUNNING" {
		return ErrProtocol
	}
	if !receipt.Recorded {
		if receipt.State != "UNREPORTED" || receipt.OwnerState == "SUCCEEDED" || receipt.Applied || receipt.Observation.Outcome != "" || len(receipt.Observation.Result) != 0 || receipt.Observation.ResultDigest != "" {
			return ErrProtocol
		}
		if !receipt.AuthorizedUntil.IsZero() && (now.IsZero() || !receipt.AuthorizedUntil.After(now) || receipt.AuthorizedUntil.After(now.Add(30*time.Second))) {
			return ErrProtocol
		}
		return nil
	}
	if !receipt.AuthorizedUntil.IsZero() {
		return ErrProtocol
	}
	expected := string(receipt.Observation.Outcome)
	switch receipt.Observation.Outcome {
	case ExecutionSucceeded:
		if !Digest(receipt.Observation.ResultDigest) {
			return ErrProtocol
		}
	case ExecutionFailed:
		if receipt.Observation.ResultDigest != "" && !Digest(receipt.Observation.ResultDigest) {
			return ErrProtocol
		}
	case ExecutionUnknown:
		expected = "UNKNOWN_OUTCOME"
		if receipt.Observation.ResultDigest != "" || len(receipt.Observation.Result) != 0 {
			return ErrProtocol
		}
	default:
		return ErrProtocol
	}
	if receipt.Applied && receipt.State != expected || !receipt.Applied && receipt.State != "DISCARDED" || !receipt.Applied && len(receipt.Observation.Result) != 0 || !withResult && len(receipt.Observation.Result) != 0 {
		return ErrProtocol
	}
	if len(receipt.Observation.Result) != 0 && receipt.Observation.Validate() != nil {
		return ErrProtocol
	}
	return nil
}

// String conceals private output, intent coordinates and deadline pins in ordinary diagnostics.
// String скрывает private output, intent coordinates и deadline pins в ordinary diagnostics.
func (ExecutionReceipt) String() string { return "[SSH owner execution receipt]" }

// GoString retains the same privacy boundary for detailed diagnostic formatting.
// GoString сохраняет ту же privacy boundary для detailed diagnostic formatting.
func (receipt ExecutionReceipt) GoString() string { return receipt.String() }

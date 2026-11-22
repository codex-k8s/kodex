package managedkubernetes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// StreamEffectResult is a bounded projection of a validated terminal receipt, not delivery authority.
// Callers must obtain the receipt from the original owner graph and separately check fresh output credit.
// StreamEffectResult — bounded projection проверенной terminal receipt, не delivery authority.
// Callers получают receipt из original owner graph и отдельно проверяют fresh output credit.
type StreamEffectResult struct {
	ResultJSON                        []byte `json:"-"`
	ResultDigest, Outcome, ReasonCode string
}

// streamEffectWire excludes output, owner references, transport details and private identity metadata.
// streamEffectWire исключает output, owner references, transport details и private identity metadata.
type streamEffectWire struct {
	Stream streamEffectState `json:"stream"`
}

// streamEffectState keeps local delivery state independent from the actual observed remote outcome.
// streamEffectState отделяет local delivery state от actual observed remote outcome.
type streamEffectState struct {
	LocalState    string `json:"localState"`
	RemoteOutcome string `json:"remoteOutcome"`
	ExitCode      *int32 `json:"exitCode,omitempty"`
}

// BuildStreamEffectResult emits one canonical safe projection from protocol-validated terminal facts.
// A successful remote exit with LOST output stays remote SUCCEEDED and local LOST; the caller must
// not report complete delivery from Outcome alone. UNKNOWN never invents an exit code.
// BuildStreamEffectResult создаёт одну canonical safe projection из protocol-validated terminal facts.
// Successful remote exit при LOST output остаётся remote SUCCEEDED и local LOST; caller не вправе
// объявлять полную доставку только по Outcome. UNKNOWN не выдумывает exit code.
func BuildStreamEffectResult(receipt TerminalReceipt) (StreamEffectResult, error) {
	if receipt.Validate() != nil {
		return StreamEffectResult{}, ErrInvalidEffectResult
	}
	wire := streamEffectWire{Stream: streamEffectState{LocalState: receipt.LocalState, RemoteOutcome: receipt.RemoteOutcome}}
	if receipt.ExitCode != nil {
		code := *receipt.ExitCode
		wire.Stream.ExitCode = &code
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		return StreamEffectResult{}, ErrInvalidEffectResult
	}
	digest := sha256.Sum256(raw)
	result := StreamEffectResult{ResultJSON: raw, ResultDigest: hex.EncodeToString(digest[:]), Outcome: receipt.RemoteOutcome}
	if receipt.RemoteOutcome == "FAILED" {
		result.ReasonCode = ReasonRemoteFailure
	}
	if receipt.RemoteOutcome == "UNKNOWN" {
		switch receipt.ReasonCode {
		case "CANCELED":
			result.ReasonCode = ReasonStreamCanceled
		case "REVOKED":
			result.ReasonCode = ReasonGrantRevoked
		case "EXPIRED":
			result.ReasonCode = ReasonLeaseExpired
		case "LIMIT_EXCEEDED":
			result.ReasonCode = ReasonSizeLimit
		case "OUTPUT_LOST", "TRANSPORT_LOST", "OWNER_STATE_LOST":
			result.ReasonCode = ReasonStreamLost
		default:
			result.ReasonCode = ReasonTransportUnknown
		}
	}
	if ValidateEffectResult(raw, result.ResultDigest, result.Outcome, result.ReasonCode) != nil {
		return StreamEffectResult{}, ErrInvalidEffectResult
	}
	return result, nil
}

// ValidateStreamEffectResult compares exact canonical bytes and semantic fields to trusted stored facts.
// It does not decode a second permissive schema or accept a caller-provided terminal receipt as authority.
// ValidateStreamEffectResult сравнивает exact canonical bytes и semantic fields с trusted stored facts.
// Не разбирает вторую permissive schema и не принимает caller terminal receipt за authority.
func ValidateStreamEffectResult(receipt TerminalReceipt, raw []byte, digest, outcome, reason string) error {
	expected, err := BuildStreamEffectResult(receipt)
	if err != nil || !bytes.Equal(raw, expected.ResultJSON) || digest != expected.ResultDigest || outcome != expected.Outcome || reason != expected.ReasonCode {
		return ErrInvalidEffectResult
	}
	return nil
}

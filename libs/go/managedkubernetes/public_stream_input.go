package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// ValidatePublicStreamInput checks borrowed private bytes against their exact digest and public source class without retaining them or granting stream authority.
// Binary stdin is opaque; tunnel data uses a bounded mapping-slot envelope; resize uses the existing exact JSON decoder and native columns/rows bounds. FILE_CONTENT belongs only to the pinned-artifact owner reader; CONTROL is never a raw command.
// ValidatePublicStreamInput проверяет borrowed private bytes по exact digest и public source class без сохранения либо выдачи stream authority.
// Binary stdin остаётся opaque; tunnel data содержит bounded mapping-slot envelope; resize использует existing exact JSON decoder и native columns/rows limits. FILE_CONTENT принадлежит лишь pinned-artifact owner reader; CONTROL не является raw command.
func ValidatePublicStreamInput(channel string, payload []byte, payloadDigest string, endOfInput bool) error {
	if len(payload) > MaximumFrameBytes || !ValidDigest(payloadDigest) {
		return ErrInvalidOperationInput
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != payloadDigest {
		return ErrInvalidOperationInput
	}
	switch channel {
	case "PORT_FORWARD":
		frame, err := DecodePortForwardConnectionFrame(payload)
		if err == nil && !endOfInput {
			return frame.ValidateInput(MaximumForwardPorts)
		}
		return ErrInvalidOperationInput
	case "STDIN":
		if len(payload) > 0 && !endOfInput || len(payload) == 0 && endOfInput {
			return nil
		}
	case "TERMINAL_RESIZE":
		var size struct {
			Columns uint16 `json:"columns"`
			Rows    uint16 `json:"rows"`
		}
		if !endOfInput && internalrpcauth.DecodeExactJSON(payload, &size) == nil && size.Columns > 0 && size.Columns <= 1024 && size.Rows > 0 && size.Rows <= 512 {
			return nil
		}
	}
	return ErrInvalidOperationInput
}

package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// MaximumEffectResultBytes bounds the safe owner result independently of transport buffering.
// MaximumEffectResultBytes ограничивает safe owner result независимо от transport buffering.
const MaximumEffectResultBytes = 64 << 10

// ErrInvalidEffectResult hides result contents while rejecting malformed or unregistered receipts.
// ErrInvalidEffectResult скрывает result contents, отвергая malformed либо unregistered receipts.
var ErrInvalidEffectResult = errors.New("invalid managed Kubernetes effect result")

// ValidateEffectResult checks the exact byte digest, strict object JSON and closed outcome/reason.
// It grants no output credit and does not replace operation-specific projection or fresh owner checks.
// ValidateEffectResult проверяет exact byte digest, strict object JSON и closed outcome/reason.
// Не выдаёт output credit и не заменяет operation-specific projection либо fresh owner checks.
func ValidateEffectResult(raw []byte, digest, outcome, reason string) error {
	if len(raw) == 0 || len(raw) > MaximumEffectResultBytes || !utf8.Valid(raw) || !ValidDigest(digest) {
		return ErrInvalidEffectResult
	}
	switch outcome {
	case "SUCCEEDED":
		if reason != "" {
			return ErrInvalidEffectResult
		}
	case "FAILED", "UNKNOWN":
		if !ValidReasonCode(reason) {
			return ErrInvalidEffectResult
		}
	default:
		return ErrInvalidEffectResult
	}
	var projection map[string]any
	if internalrpcauth.DecodeStrictJSON(raw, &projection) != nil || projection == nil {
		return ErrInvalidEffectResult
	}
	actual := sha256.Sum256(raw)
	if hex.EncodeToString(actual[:]) != digest {
		return ErrInvalidEffectResult
	}
	return nil
}

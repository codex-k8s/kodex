package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"strings"
	"unicode/utf8"
)

// KubernetesRuntimeResult carries bounded owner output, never an execution credential or new effect credit.
// KubernetesRuntimeResult содержит ограниченный результат владельца без ключей исполнения или права на новый эффект.
type KubernetesRuntimeResult struct {
	InvocationRef, ResultSummary, SafeErrorCode, EffectReceiptRef, ResultDigest string
	State                                                                       enum.KubernetesToolState
	WaitingForGate                                                              bool
}

// Valid rejects unknown states, forged success receipts and contradictory gate or output projections.
// Valid отклоняет неизвестные состояния, подложные success receipts и противоречивые проекции Gate или результата.
func (v KubernetesRuntimeResult) Valid() bool {
	if !strings.HasPrefix(v.InvocationRef, "inv_") || !managedkubernetes.ValidLocator(v.InvocationRef) || !v.State.Known() || len(v.ResultSummary) > 64<<10 || !utf8.ValidString(v.ResultSummary) || len(v.SafeErrorCode) > 96 || v.State.Terminal() && v.WaitingForGate {
		return false
	}
	for _, c := range v.SafeErrorCode {
		if c != '_' && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	if v.State == "SUCCEEDED" {
		digest := sha256.Sum256([]byte(v.ResultSummary))
		return v.SafeErrorCode == "" && strings.HasPrefix(v.EffectReceiptRef, "erc_") && managedkubernetes.ValidLocator(v.EffectReceiptRef) && v.ResultDigest == hex.EncodeToString(digest[:])
	}
	return v.ResultSummary == "" && v.EffectReceiptRef == "" && v.ResultDigest == ""
}

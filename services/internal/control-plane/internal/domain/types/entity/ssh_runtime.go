package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"strings"
	"unicode/utf8"
)

// SSHRuntimeResult carries bounded owner output, never an execution credential or new effect credit.
// SSHRuntimeResult содержит ограниченный результат владельца без ключей исполнения или права на новый эффект.
type SSHRuntimeResult struct {
	InvocationRef, ResultSummary, SafeErrorCode, EffectReceiptRef, ResultDigest string
	State                                                                       string
	WaitingForGate                                                              bool
}

// Valid rejects unknown states, forged success receipts and contradictory gate or output projections.
// Valid отклоняет неизвестные состояния, подложные success receipts и противоречивые проекции Gate или результата.
func (v SSHRuntimeResult) Valid() bool {
	if !strings.HasPrefix(v.InvocationRef, "inv_") || !managedssh.OwnerReference(v.InvocationRef) || !sshRuntimeStateKnown(v.State) || len(v.ResultSummary) > 64<<10 || !utf8.ValidString(v.ResultSummary) || len(v.SafeErrorCode) > 96 || sshRuntimeStateTerminal(v.State) && v.WaitingForGate {
		return false
	}
	for _, c := range v.SafeErrorCode {
		if c != '_' && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	if v.State == "SUCCEEDED" {
		digest := sha256.Sum256([]byte(v.ResultSummary))
		return v.SafeErrorCode == "" && strings.HasPrefix(v.EffectReceiptRef, "erc_") && managedssh.OwnerReference(v.EffectReceiptRef) && v.ResultDigest == hex.EncodeToString(digest[:])
	}
	return v.ResultSummary == "" && v.EffectReceiptRef == "" && v.ResultDigest == ""
}

// sshRuntimeStateKnown keeps the owner result union closed; an unknown database state never becomes pending or successful.
// sshRuntimeStateKnown сохраняет закрытый owner result union; неизвестный database state не становится pending либо successful.
func sshRuntimeStateKnown(state string) bool {
	switch state {
	case "READY", "RUNNING", "WAITING_APPROVAL", "SUCCEEDED", "FAILED", "REJECTED", "CANCELLED", "UNKNOWN_OUTCOME":
		return true
	}
	return false
}

// sshRuntimeStateTerminal distinguishes completed owner metadata from a live invocation without granting remote quiescence.
// sshRuntimeStateTerminal отличает completed owner metadata от live invocation, не подтверждая remote quiescence.
func sshRuntimeStateTerminal(state string) bool {
	return sshRuntimeStateKnown(state) && state != "READY" && state != "RUNNING" && state != "WAITING_APPROVAL"
}

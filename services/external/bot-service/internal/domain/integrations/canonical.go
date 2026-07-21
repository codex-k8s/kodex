package integrations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	connectionPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,79}$`)
	namespacePattern    = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)
	workloadNamePattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9.]{0,251}[a-z0-9])?$`)
	idempotencyPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
)

type approvalBindingEnvelope struct {
	Version            int    `json:"version"`
	InvocationID       string `json:"invocation_id"`
	CapabilityKey      string `json:"capability_key"`
	CapabilityVersion  int    `json:"capability_version"`
	CapabilityRevision int64  `json:"capability_revision"`
	ConnectionID       string `json:"connection_id"`
	ConnectionRevision int64  `json:"connection_revision"`
	GrantID            string `json:"grant_id"`
	GrantRevision      int64  `json:"grant_revision"`
	SubjectKind        string `json:"subject_kind"`
	SubjectRef         string `json:"subject_ref"`
	InstallationScope  string `json:"installation_scope"`
	WorkspaceScope     string `json:"workspace_scope"`
	SessionScope       string `json:"session_scope"`
	ArgumentsHash      string `json:"arguments_sha256"`
}

func validateRestartInput(input RestartWorkloadInput) (RestartArguments, error) {
	input.Connection = strings.TrimSpace(input.Connection)
	input.Namespace = strings.TrimSpace(input.Namespace)
	input.WorkloadKind = strings.TrimSpace(input.WorkloadKind)
	input.WorkloadName = strings.TrimSpace(input.WorkloadName)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if !connectionPattern.MatchString(input.Connection) || !namespacePattern.MatchString(input.Namespace) ||
		input.WorkloadKind != "Deployment" || !workloadNamePattern.MatchString(input.WorkloadName) ||
		!idempotencyPattern.MatchString(input.IdempotencyKey) {
		return RestartArguments{}, ErrInvalidInput
	}
	return RestartArguments{
		Namespace: input.Namespace, WorkloadKind: input.WorkloadKind, WorkloadName: input.WorkloadName,
	}, nil
}

func hashArguments(arguments RestartArguments) (string, error) {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return "", fmt.Errorf("encode restart arguments: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// ApprovalBindingHash вычисляет versioned digest для repository T1.
func ApprovalBindingHash(session SessionContext, binding Binding, invocationID string, argumentsHash string) (string, error) {
	envelope := approvalBindingEnvelope{
		Version: 1, InvocationID: invocationID,
		CapabilityKey: binding.CapabilityKey, CapabilityVersion: binding.CapabilityVersion,
		CapabilityRevision: binding.CapabilityRevision,
		ConnectionID:       binding.ConnectionPublicID, ConnectionRevision: binding.ConnectionRevision,
		GrantID: binding.GrantPublicID, GrantRevision: binding.GrantRevision,
		SubjectKind: session.SubjectKind, SubjectRef: session.SubjectRef,
		InstallationScope: session.InstallationScope, WorkspaceScope: session.WorkspaceScope,
		SessionScope: session.SessionKey, ArgumentsHash: argumentsHash,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("encode approval binding: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

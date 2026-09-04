package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

var runtimeCallbackMethods = [...]string{
	"GET artifact",
	"POST complete",
	"POST mcp",
	"POST native-tool-call",
	"POST progress",
	"POST provider-credential-refresh",
}

type runtimeExecutionIdentity struct {
	OrganizationRef        string `json:"organization_ref"`
	ProjectRef             string `json:"project_ref"`
	RunRef                 string `json:"run_ref"`
	NodeRef                string `json:"node_ref"`
	SessionRef             string `json:"session_ref"`
	TurnRef                string `json:"turn_ref"`
	Attempt                int32  `json:"attempt"`
	LeaseRef               string `json:"lease_ref"`
	LeaseFence             string `json:"lease_fence"`
	LeaseGeneration        int64  `json:"lease_generation"`
	RuntimeRevisionRef     string `json:"runtime_revision_ref"`
	RuntimeRevisionVersion int64  `json:"runtime_revision_version"`
	RuntimeRevisionDigest  string `json:"runtime_revision_digest"`
	InputDigest            string `json:"input_digest"`
}

func RuntimeExecutionBindingDigests(input RunnerInput) (string, string, error) {
	identity := runtimeExecutionIdentity{OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, RunRef: input.RunRef, NodeRef: input.NodeRef,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt, LeaseRef: input.LeaseRef,
		LeaseFence:      input.LeaseFence,
		LeaseGeneration: input.LeaseGeneration, RuntimeRevisionRef: input.RuntimeRevisionRef,
		RuntimeRevisionVersion: input.RuntimeRevisionVersion, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		InputDigest: input.InputDigest}
	if identity.OrganizationRef == "" || identity.ProjectRef == "" || identity.RunRef == "" || identity.NodeRef == "" || identity.SessionRef == "" ||
		identity.TurnRef == "" || identity.Attempt < 1 || identity.LeaseRef == "" || identity.LeaseFence == "" || identity.LeaseGeneration < 1 ||
		identity.RuntimeRevisionRef == "" || identity.RuntimeRevisionVersion < 1 || identity.RuntimeRevisionDigest == "" || identity.InputDigest == "" {
		return "", "", errors.New("runtime execution identity is incomplete")
	}
	executionRaw, err := json.Marshal(struct {
		Identity runtimeExecutionIdentity `json:"identity"`
		Methods  []string                 `json:"methods"`
	}{Identity: identity, Methods: runtimeCallbackMethods[:]})
	if err != nil {
		return "", "", errors.New("encode runtime execution binding")
	}
	executionSum := sha256.Sum256(executionRaw)
	mcpRaw, err := json.Marshal(struct {
		Identity     runtimeExecutionIdentity `json:"identity"`
		Capabilities []string                 `json:"capabilities"`
		Grants       []RunnerIntegrationGrant `json:"grants"`
	}{Identity: identity, Capabilities: input.Capabilities, Grants: input.IntegrationGrants})
	if err != nil {
		return "", "", errors.New("encode runtime MCP binding")
	}
	mcpSum := sha256.Sum256(mcpRaw)
	return hex.EncodeToString(executionSum[:]), hex.EncodeToString(mcpSum[:]), nil
}

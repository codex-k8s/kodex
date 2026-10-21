package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// runtimeCallbackMethods pins the closed callback operation set into each execution digest; this list does not itself authorize a request.
// runtimeCallbackMethods закрепляет закрытый набор callback-операций в дайджесте исполнения; сам список не авторизует запрос.
var runtimeCallbackMethods = [...]string{
	"GET artifact",
	"POST complete",
	"POST mcp",
	"POST native-tool-call",
	"POST progress",
	"POST provider-credential-refresh",
}

// runtimeExecutionIdentity collects the scope, attempt, lease and revision fields required before computing an execution binding.
// runtimeExecutionIdentity собирает область, попытку, аренду и ревизию, обязательные перед вычислением привязки исполнения.
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

// RuntimeExecutionBindingDigests validates required turn identity and hashes the full materialization with separate callback and MCP method bindings, excluding the resulting digests themselves.
// RuntimeExecutionBindingDigests проверяет обязательную идентичность хода и хэширует полную материализацию с отдельными привязками callback и MCP, исключая из входа сами результирующие дайджесты.
func RuntimeExecutionBindingDigests(input RunnerInput) (string, string, error) {
	if input.validateAssistantScope() != nil {
		return "", "", errors.New("runtime execution assistant scope is invalid")
	}
	identity := runtimeExecutionIdentity{OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, RunRef: input.RunRef, NodeRef: input.NodeRef,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt, LeaseRef: input.LeaseRef,
		LeaseFence:      input.LeaseFence,
		LeaseGeneration: input.LeaseGeneration, RuntimeRevisionRef: input.RuntimeRevisionRef,
		RuntimeRevisionVersion: input.RuntimeRevisionVersion, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		InputDigest: input.InputDigest}
	if input.Mode != RunnerModeTurn || input.WorkloadInstance == "" || identity.OrganizationRef == "" || identity.ProjectRef == "" && !input.IsSystemAssistant() || identity.RunRef == "" || identity.NodeRef == "" || identity.SessionRef == "" ||
		identity.TurnRef == "" || identity.Attempt < 1 || identity.LeaseRef == "" || identity.LeaseFence == "" || identity.LeaseGeneration < 1 ||
		identity.RuntimeRevisionRef == "" || identity.RuntimeRevisionVersion < 1 || identity.RuntimeRevisionDigest == "" || identity.InputDigest == "" {
		return "", "", errors.New("runtime execution identity is incomplete")
	}
	// Одинаковая JSON-модель materialization и wire input сохраняет семантику
	// пустых optional collections после encode/decode без второго списка полей.
	materialization := input
	materialization.ExecutionBindingDigest, materialization.MCPBindingDigest = "", ""
	executionRaw, err := json.Marshal(struct {
		Materialization RunnerInput `json:"materialization"`
		Methods         []string    `json:"methods"`
	}{Materialization: materialization, Methods: runtimeCallbackMethods[:]})
	if err != nil {
		return "", "", errors.New("encode runtime execution binding")
	}
	executionSum := sha256.Sum256(executionRaw)
	mcpRaw, err := json.Marshal(struct {
		Materialization RunnerInput `json:"materialization"`
		Method          string      `json:"method"`
	}{Materialization: materialization, Method: "POST mcp"})
	if err != nil {
		return "", "", errors.New("encode runtime MCP binding")
	}
	mcpSum := sha256.Sum256(mcpRaw)
	return hex.EncodeToString(executionSum[:]), hex.EncodeToString(mcpSum[:]), nil
}

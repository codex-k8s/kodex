package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

const (
	providerInputProofMessage  = "Codex provider input acknowledged"
	providerInputProofEventKey = "event"
	providerInputProofEvent    = "PROVIDER_INPUT_ACKNOWLEDGED"
	providerInputProofKey      = "proof"
	proofEqual                 = "EQUAL"
	proofDifferent             = "DIFFERENT"
	proofUnavailable           = "UNAVAILABLE"
)

// Закрытая проекция actual turn/start, не сериализация RunnerInput либо wire body.
// Hashes предназначены для привилегированного QA read, не для публичного каталога.
type providerInputProof struct {
	RunRef                      string               `json:"run_ref"`
	NodeRef                     string               `json:"node_ref"`
	SessionRef                  string               `json:"session_ref"`
	TurnRef                     string               `json:"turn_ref"`
	Attempt                     int32                `json:"attempt"`
	LeaseRef                    string               `json:"lease_ref"`
	LeaseGeneration             int64                `json:"lease_generation"`
	InputDigest                 string               `json:"input_digest"`
	AgentRef                    string               `json:"agent_ref"`
	OrganizationRef             string               `json:"organization_ref"`
	ProjectRef                  string               `json:"project_ref,omitempty"`
	AssistantScope              string               `json:"assistant_scope"`
	RuntimeRevisionRef          string               `json:"runtime_revision_ref"`
	RuntimeRevisionVersion      int64                `json:"runtime_revision_version"`
	RuntimeRevisionDigest       string               `json:"runtime_revision_digest"`
	RuntimeConfigRef            string               `json:"runtime_config_ref"`
	RuntimeConfigVersion        int64                `json:"runtime_config_version"`
	RuntimeConfigDigest         string               `json:"runtime_config_digest"`
	EnvironmentRef              string               `json:"environment_ref"`
	EnvironmentVersion          int64                `json:"environment_version"`
	EnvironmentDigest           string               `json:"environment_digest"`
	EnvironmentBindingRef       string               `json:"environment_binding_ref"`
	EnvironmentBindingVersion   int64                `json:"environment_binding_version"`
	EnvironmentBindingDigest    string               `json:"environment_binding_digest"`
	ImageReference              string               `json:"image_reference"`
	ImageManifestDigest         string               `json:"image_manifest_digest"`
	ImageArtifactRef            string               `json:"image_artifact_ref,omitempty"`
	ImageRecipeRef              string               `json:"image_recipe_ref,omitempty"`
	ImageRecipeGeneration       int64                `json:"image_recipe_generation,omitempty"`
	Model                       string               `json:"model"`
	ReasoningEffort             string               `json:"reasoning_effort"`
	ReasoningMode               string               `json:"reasoning_mode"`
	InstructionRef              string               `json:"instruction_ref"`
	InstructionDigest           string               `json:"instruction_digest"`
	PromptTemplateRef           string               `json:"prompt_template_ref"`
	PromptTemplateDigest        string               `json:"prompt_template_digest"`
	PromptMaterializationDigest string               `json:"prompt_materialization_digest"`
	TaskSHA256                  string               `json:"task_sha256"`
	TaskInPrompt                bool                 `json:"task_in_prompt"`
	ProviderPromptSHA256        string               `json:"provider_prompt_sha256"`
	ProviderPromptBytes         int                  `json:"provider_prompt_bytes"`
	InstructionsSHA256          string               `json:"instructions_sha256"`
	InstructionsBytes           int                  `json:"instructions_bytes"`
	InstructionsFileSHA256      string               `json:"instructions_file_sha256,omitempty"`
	InstructionsFileComparison  string               `json:"instructions_file_comparison"`
	InboxPromptSHA256           string               `json:"inbox_prompt_sha256,omitempty"`
	InboxPromptComparison       string               `json:"inbox_prompt_comparison"`
	ExecutionBindingDigest      string               `json:"execution_binding_digest"`
	MCPBindingDigest            string               `json:"mcp_binding_digest"`
	Tools                       []providerProofTool  `json:"tools"`
	Grants                      []providerProofGrant `json:"grants"`
	Capabilities                []string             `json:"capabilities"`
}

type providerProofTool struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

type providerProofGrant struct {
	Ref               string `json:"ref"`
	Version           int64  `json:"version"`
	ConnectionRef     string `json:"connection_ref"`
	ConnectionVersion int64  `json:"connection_version"`
	CapabilityKey     string `json:"capability_key"`
	ApprovalPolicy    string `json:"approval_policy"`
}

type providerInputProofObserver func(context.Context, providerInputProof)

func providerInputProofLogger(logger *slog.Logger) providerInputProofObserver {
	return func(ctx context.Context, proof providerInputProof) {
		logger.LogAttrs(ctx, slog.LevelInfo, providerInputProofMessage,
			slog.String(providerInputProofEventKey, providerInputProofEvent), slog.Any(providerInputProofKey, proof))
	}
}

// Даже warm consumer использует здесь текущий verified in-memory TURN, а не
// mounted WARM runtime.json. Receipt появляется только после успешного ACK.
func (server *appServer) startTurnWithInputProof(ctx context.Context, state *protocolState, input model.Input, prompt []byte, observer providerInputProofObserver) error {
	params, err := turnStartParams(input, state.threadID, prompt)
	if err != nil {
		return atProviderStage(providerStageTurnParameters, err)
	}
	var proof providerInputProof
	if observer != nil {
		proof, err = newProviderInputProof(input, params)
		if err != nil {
			return atProviderStage(providerStageTurnParameters, err)
		}
	}
	raw, err := server.call(ctx, state, "turn/start", params)
	if err != nil {
		return atProviderStage(providerStageTurnStart, err)
	}
	if err := state.bindTurn(raw); err != nil {
		return atProviderStage(providerStageTurnStart, err)
	}
	if observer != nil {
		observer(ctx, proof)
	}
	return nil
}

func newProviderInputProof(input model.Input, params map[string]any) (providerInputProof, error) {
	invalid := errors.New("provider input proof is invalid")
	items, ok := params["input"].([]map[string]any)
	if !ok || len(items) == 0 || items[0]["type"] != "text" {
		return providerInputProof{}, invalid
	}
	text, ok := items[0]["text"].(string)
	modelName, modelOK := params["model"].(string)
	effort := ""
	if value, exists := params["effort"]; exists {
		var effortOK bool
		effort, effortOK = value.(string)
		if !effortOK {
			return providerInputProof{}, invalid
		}
	}
	if !ok || len(text) == 0 || len(text) > 1<<20 || !modelOK || modelName != input.Model ||
		(input.ReasoningMode == runtimecontract.ReasoningSupported && effort != input.EffectiveReasoningEffort) {
		return providerInputProof{}, invalid
	}
	promptBytes, instructions := []byte(text), []byte(input.Instructions)
	proof := providerInputProof{
		RunRef: input.RunRef, NodeRef: input.NodeRef, SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt,
		LeaseRef: input.LeaseRef, LeaseGeneration: input.LeaseGeneration, InputDigest: input.InputDigest,
		AgentRef: input.AgentRef, OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, AssistantScope: string(input.AssistantScope),
		RuntimeRevisionRef: input.RuntimeRevisionRef, RuntimeRevisionVersion: input.RuntimeRevisionVersion, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		RuntimeConfigRef: input.RuntimeConfigRef, RuntimeConfigVersion: input.RuntimeConfigVersion, RuntimeConfigDigest: input.RuntimeConfigDigest,
		EnvironmentRef: input.RuntimeEnvironmentRef, EnvironmentVersion: input.RuntimeEnvironmentVersion, EnvironmentDigest: input.RuntimeEnvironmentDigest,
		EnvironmentBindingRef: input.EnvironmentBindingRef, EnvironmentBindingVersion: input.EnvironmentBindingVersion, EnvironmentBindingDigest: input.EnvironmentBindingDigest,
		ImageReference: input.ImageReference, ImageManifestDigest: input.ImageManifestDigest, ImageArtifactRef: input.EnvironmentImage.ArtifactRef,
		ImageRecipeRef: input.EnvironmentImage.RecipeRef, ImageRecipeGeneration: input.EnvironmentImage.RecipeGeneration,
		Model: modelName, ReasoningEffort: effort, ReasoningMode: input.ReasoningMode,
		InstructionRef: input.InstructionRef, InstructionDigest: input.InstructionDigest,
		PromptTemplateRef: input.PromptTemplateRef, PromptTemplateDigest: input.PromptTemplateDigest, PromptMaterializationDigest: input.PromptMaterializationDigest,
		TaskSHA256: providerProofSHA([]byte(input.Task)), TaskInPrompt: input.Task != "" && bytes.Contains(promptBytes, []byte(input.Task)),
		ProviderPromptSHA256: providerProofSHA(promptBytes), ProviderPromptBytes: len(promptBytes),
		InstructionsSHA256: providerProofSHA(instructions), InstructionsBytes: len(instructions),
		ExecutionBindingDigest: input.ExecutionBindingDigest, MCPBindingDigest: input.MCPBindingDigest,
		Tools: []providerProofTool{}, Grants: []providerProofGrant{}, Capabilities: append([]string{}, input.Capabilities...),
	}
	proof.InstructionsFileSHA256, proof.InstructionsFileComparison = compareProviderProofFile(input.WorkspaceRoot, "AGENTS.md", instructions)
	proof.InboxPromptSHA256, proof.InboxPromptComparison = compareProviderProofFile(input.WorkspaceRoot, ".kodex/inbox/prompt.md", promptBytes)
	for _, tool := range input.EnvironmentTools {
		proof.Tools = append(proof.Tools, providerProofTool{tool.Name, tool.Command})
	}
	for _, grant := range input.IntegrationGrants {
		proof.Grants = append(proof.Grants, providerProofGrant{grant.Ref, grant.GrantVersion, grant.ConnectionRef, grant.ConnectionVersion, grant.CapabilityKey, grant.ApprovalPolicy})
	}
	return proof, nil
}

func providerProofSHA(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func compareProviderProofFile(root, relative string, expected []byte) (string, string) {
	raw, err := readProviderProofFile(root, relative)
	if err != nil {
		return "", proofUnavailable
	}
	comparison := proofDifferent
	if bytes.Equal(raw, expected) {
		comparison = proofEqual
	}
	return providerProofSHA(raw), comparison
}

// Только два фиксированных пути, каждый компонент O_NOFOLLOW; никаких env,
// credential files, произвольных путей либо symlink aliases в debug reader.
func readProviderProofFile(root, relative string) ([]byte, error) {
	invalid := errors.New("provider proof file is unavailable")
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || (relative != "AGENTS.md" && relative != ".kodex/inbox/prompt.md") {
		return nil, invalid
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, invalid
	}
	defer func() { _ = unix.Close(fd) }()
	parts := strings.Split(relative, "/")
	for _, part := range parts[:len(parts)-1] {
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, invalid
		}
		_ = unix.Close(fd)
		fd = next
	}
	leaf, err := unix.Openat(fd, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, invalid
	}
	file := os.NewFile(uintptr(leaf), relative)
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() > 1<<20 {
		return nil, invalid
	}
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	after, statErr := file.Stat()
	if err != nil || statErr != nil || len(raw) > 1<<20 || int64(len(raw)) != before.Size() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, invalid
	}
	return raw, nil
}

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
	// providerInputProofMessage fixes the diagnostic message for an acknowledged turn/start projection.
	// providerInputProofMessage фиксирует diagnostic message подтверждённой проекции turn/start.
	providerInputProofMessage = "Codex provider input acknowledged"
	// providerInputProofEventKey names the structured event field rather than deriving it from payload.
	// providerInputProofEventKey задаёт имя структурированного event field, не получая его из payload.
	providerInputProofEventKey = "event"
	// providerInputProofEvent identifies the bounded input-acknowledgement record, not turn completion.
	// providerInputProofEvent обозначает ограниченную запись input acknowledgement, не завершение хода.
	providerInputProofEvent = "PROVIDER_INPUT_ACKNOWLEDGED"
	// providerInputProofKey names the structured projection field in the privileged diagnostic record.
	// providerInputProofKey задаёт имя поля проекции в привилегированной diagnostic record.
	providerInputProofKey = "proof"
	// proofEqual means the bounded file bytes equal the supplied expected bytes at this read.
	// proofEqual означает равенство ограниченных байтов файла переданным expected bytes при этом чтении.
	proofEqual = "EQUAL"
	// proofDifferent means a readable bounded file differs from the expected byte sequence.
	// proofDifferent означает отличие прочитанного ограниченного файла от expected byte sequence.
	proofDifferent = "DIFFERENT"
	// proofUnavailable reports an unreadable or rejected comparison source without its path or OS error.
	// proofUnavailable отмечает недоступный или отклонённый источник сравнения без пути и OS error.
	proofUnavailable = "UNAVAILABLE"
)

// providerInputProof is a closed privileged diagnostic projection of prepared TURN input and the
// actual first text/model/effort parameters acknowledged by the provider path. Hashes and identifiers
// are not public-catalog data; the record is not cryptographic provider attestation, full wire-body
// serialization, a grant, or proof that the turn completed or its files were published.
// providerInputProof — закрытая привилегированная diagnostic projection подготовленного TURN input
// и фактических first text/model/effort parameters, подтверждённых provider path. Hashes и identifiers
// не предназначены для публичного каталога; запись не является криптографической аттестацией provider,
// полной сериализацией wire body, grant или доказательством завершения хода и публикации файлов.
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

// providerProofTool retains a configured tool name/command, excluding its description and call arguments;
// the diagnostic itself does not authorize executing that command.
// providerProofTool сохраняет настроенные tool name/command без description и call arguments;
// сама диагностика не разрешает выполнение этой команды.
type providerProofTool struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// providerProofGrant projects grant/connection versions, capability and policy for privileged diagnosis;
// it contains no credential and cannot replace the authoritative runtime grant.
// providerProofGrant проецирует grant/connection versions, capability и policy для приватной диагностики;
// credential в ней нет, и авторитетный runtime grant она не заменяет.
type providerProofGrant struct {
	Ref               string `json:"ref"`
	Version           int64  `json:"version"`
	ConnectionRef     string `json:"connection_ref"`
	ConnectionVersion int64  `json:"connection_version"`
	CapabilityKey     string `json:"capability_key"`
	ApprovalPolicy    string `json:"approval_policy"`
}

// providerInputProofObserver receives the prepared projection synchronously after a valid start ACK;
// it has no error return, delivery acknowledgement or retry contract.
// providerInputProofObserver синхронно получает подготовленную проекцию после корректного start ACK;
// error return, delivery acknowledgement и retry contract отсутствуют.
type providerInputProofObserver func(context.Context, providerInputProof)

// providerInputProofLogger adapts a nonnil trusted logger to the observer using fixed event fields.
// It logs the supplied closed projection as-is, not arbitrary payloads through a general redactor.
// providerInputProofLogger адаптирует ненулевой доверенный logger к observer с фиксированными event fields.
// Переданная закрытая проекция пишется как есть; это не общий redactor произвольных payloads.
func providerInputProofLogger(logger *slog.Logger) providerInputProofObserver {
	return func(ctx context.Context, proof providerInputProof) {
		logger.LogAttrs(ctx, slog.LevelInfo, providerInputProofMessage,
			slog.String(providerInputProofEventKey, providerInputProofEvent), slog.Any(providerInputProofKey, proof))
	}
}

// startTurnWithInputProof derives current turn parameters and, when requested, a projection from
// verified in-memory input rather than mounted WARM state. It emits only after turn/start succeeds
// and binds an in-progress turn; an ACK is not completion, billing or durable log-delivery proof.
// startTurnWithInputProof строит текущие turn parameters и необязательную проекцию из проверенного
// in-memory input, не mounted WARM state. Выдача происходит лишь после успешного turn/start
// и binding in-progress turn; ACK не доказывает завершение, биллинг или устойчивую доставку лога.
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

// newProviderInputProof expects already verified input and bounds/checks the first text item,
// exact model and supported reasoning effort. It hashes that text and supplied instructions, tests
// task byte inclusion, compares two fixed files and copies diagnostic metadata. It does not validate
// every input field/additional item, authenticate the provider or prove semantic task execution.
// newProviderInputProof требует уже проверенный input и ограничивает/проверяет первый text item,
// точную model и поддержанный reasoning effort. Она хеширует text и переданные instructions, проверяет
// включение байтов task, сравнивает два фиксированных файла и копирует diagnostic metadata. Все
// input fields/additional items, provider authentication и смысловое выполнение задачи не проверяются.
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

// providerProofSHA computes the lowercase SHA-256 hex of exact bytes, including an empty input;
// a digest identifies content but does not authenticate its origin.
// providerProofSHA вычисляет lowercase SHA-256 hex точных байтов, включая пустой input;
// digest обозначает содержимое, но не подтверждает его происхождение.
func providerProofSHA(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// compareProviderProofFile hashes a successfully read fixed file and returns EQUAL/DIFFERENT;
// any read-policy failure becomes an empty digest plus UNAVAILABLE, never false equality.
// compareProviderProofFile хеширует прочитанный фиксированный файл и возвращает EQUAL/DIFFERENT;
// отказ read policy даёт пустой digest и UNAVAILABLE, не ложное равенство.
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

// readProviderProofFile permits only AGENTS.md and the inbox prompt beneath a clean absolute root.
// The root leaf and relative directory/file components use O_NOFOLLOW; a bounded regular file is
// read through its fd and checked for size/mtime changes. This diagnostic reader does not validate
// owner/link count, all root ancestors, mounts or future mutations, and grants no publication authority.
// readProviderProofFile разрешает лишь AGENTS.md и inbox prompt под чистым абсолютным root.
// Root leaf и относительные directory/file components открываются с O_NOFOLLOW; bounded regular file
// читается через fd с проверкой size/mtime. Diagnostic reader не проверяет owner/link count,
// все предки root, mounts или будущие изменения и не выдаёт publication authority.
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

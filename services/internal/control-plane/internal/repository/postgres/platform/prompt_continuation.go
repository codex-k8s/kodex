package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_continuation_previous.sql
var queryPromptContinuationPrevious string

//go:embed sql/prompt_continuation_template.sql
var queryPromptContinuationTemplate string

//go:embed sql/prompt_continuation_insert.sql
var queryPromptContinuationInsert string

const defaultContinuationTemplateRef = "prompt_continuation_default_v1"
const defaultContinuationTemplate = "Continue the Session with the current approved runtime context.\n{{slot \"RUNTIME_CHANGES\"}}\n{{slot \"INPUT\"}}"

type preparedContinuationNotice struct {
	PreviousID      string
	Diff            promptservice.RuntimeDiff
	Snapshot        entity.PromptMaterializationSnapshot
	Materialization promptservice.Materialization
}

func (repository *Repository) prepareRuntimeContinuationNotice(ctx context.Context, tx pgx.Tx, current scope,
	snapshot map[string]any, promptSnapshot entity.PromptMaterializationSnapshot,
) (*preparedContinuationNotice, error) {
	var previousID, previousRef string
	var rawPrevious []byte
	if err := tx.QueryRow(ctx, queryPromptContinuationPrevious, pgx.StrictNamedArgs{"organization_id": current.organizationID, "session_ref": stringMap(snapshot, "sessionRef")}).Scan(&previousID, &previousRef, &rawPrevious); err != nil {
		return nil, errs.ErrConflict
	}
	var previous map[string]any
	if json.Unmarshal(rawPrevious, &previous) != nil {
		return nil, errs.ErrConflict
	}
	prior, err := continuationComponents(previous)
	if err != nil {
		return nil, err
	}
	next, err := continuationComponents(snapshot)
	if err != nil {
		return nil, err
	}
	diff, err := promptservice.CompareRuntimeContexts(prior, next, promptservice.RuntimeDiff{PreviousRevisionRef: previousRef,
		CurrentRevisionRef: stringMap(snapshot, "runtimeRevisionRef"), SessionRef: stringMap(snapshot, "sessionRef"), TurnRef: stringMap(snapshot, "turnRef"), Attempt: int32(runtimeRevisionMapInt64(snapshot, "attempt"))})
	if err != nil {
		return nil, fmt.Errorf("compare continuation runtime (%v): %w", err, errs.ErrConflict)
	}
	var noticeSnapshot entity.PromptMaterializationSnapshot
	raw, err := json.Marshal(promptSnapshot)
	if err != nil || json.Unmarshal(raw, &noticeSnapshot) != nil {
		return nil, errs.ErrConflict
	}
	content := defaultContinuationTemplate
	noticeSnapshot.TemplateRef = defaultContinuationTemplateRef
	digest := sha256.Sum256([]byte(content))
	noticeSnapshot.TemplateDigest = hex.EncodeToString(digest[:])
	err = tx.QueryRow(ctx, queryPromptContinuationTemplate, pgx.StrictNamedArgs{"organization_id": current.organizationID, "agent_ref": stringMap(snapshot, "agentRef")}).Scan(&noticeSnapshot.TemplateRef, &noticeSnapshot.TemplateDigest, &content)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrUnavailable
	}
	if len(content) > 16<<10 {
		return nil, errs.ErrConflict
	}
	digest = sha256.Sum256([]byte(content))
	if noticeSnapshot.TemplateDigest != hex.EncodeToString(digest[:]) {
		return nil, errs.ErrConflict
	}
	rawDiff, err := json.Marshal(diff)
	if err != nil {
		return nil, errs.ErrConflict
	}
	noticeSnapshot.TemplateContent = content
	noticeSnapshot.ServiceTemplateRevision = promptservice.ServiceTemplateRevision
	noticeSnapshot.TargetKind, noticeSnapshot.TargetRef = promptservice.TargetSessionContinuation, stringMap(snapshot, "sessionRef")
	noticeSnapshot.StagePurposeTemplate, noticeSnapshot.StageExpectedResultTemplate = "", ""
	noticeSnapshot.ContextPin.PreviousRuntimeRevisionRef = previousRef
	noticeSnapshot.SessionContinuation = string(rawDiff)
	noticeSnapshot.SemanticValues = nil
	noticeSnapshot.UnavailableVariables = nil
	materialized, err := promptservice.Materialize(content, promptservice.FromSnapshot(noticeSnapshot))
	if err != nil || !materialized.Complete || len(materialized.Prompt) > 64<<10 {
		return nil, fmt.Errorf("materialize continuation template: %w", errs.ErrConflict)
	}
	messages, ok := snapshot["sessionContext"].([]map[string]string)
	if !ok && snapshot["sessionContext"] != nil {
		return nil, errs.ErrConflict
	}
	if len(messages) >= 128 {
		return nil, errs.ErrConflict
	}
	snapshot["sessionContext"] = append(append([]map[string]string{}, messages...), map[string]string{"role": "USER", "content": materialized.Prompt})
	snapshot["continuationPromptSnapshot"], snapshot["continuationRuntimeDiff"], snapshot["continuationNoticeDigest"] = noticeSnapshot, diff, materialized.Digest
	return &preparedContinuationNotice{PreviousID: previousID, Diff: diff, Snapshot: noticeSnapshot, Materialization: materialized}, nil
}

func (repository *Repository) persistRuntimeContinuationNotice(ctx context.Context, tx pgx.Tx, current scope, currentID string, notice *preparedContinuationNotice) error {
	if notice == nil {
		return nil
	}
	raw, err := json.Marshal(notice.Snapshot)
	if err != nil {
		return errs.ErrConflict
	}
	result, err := tx.Exec(ctx, queryPromptContinuationInsert, pgx.StrictNamedArgs{"organization_id": current.organizationID, "current_id": currentID, "previous_id": notice.PreviousID,
		"template_ref": notice.Materialization.TemplateRef, "template_digest": notice.Materialization.TemplateDigest,
		"service_revision": notice.Materialization.ServiceTemplateRevision, "service_digest": notice.Materialization.ServiceTemplateDigest,
		"variable_digest": notice.Materialization.VariableSnapshotDigest, "diff_digest": notice.Diff.Digest, "materialization_digest": notice.Materialization.Digest,
		"content": notice.Materialization.Prompt, "snapshot": raw})
	if err != nil || result.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

func continuationComponents(snapshot map[string]any) (map[string][]promptservice.RuntimeDescriptor, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, errs.ErrConflict
	}
	var data map[string]any
	if json.Unmarshal(encoded, &data) != nil {
		return nil, errs.ErrConflict
	}
	result := map[string][]promptservice.RuntimeDescriptor{}
	for _, name := range []string{"INSTRUCTIONS", "MODEL", "REASONING", "IMAGE", "ENVIRONMENT", "FILES", "SKILLS", "MEMORY", "TOOLS", "MCP", "INTEGRATIONS", "CAPABILITIES", "POLICY"} {
		result[name] = []promptservice.RuntimeDescriptor{}
	}
	value := func(name string) string { text, _ := data[name].(string); return text }
	tuple := func(ref, version, digest string) promptservice.RuntimeDescriptor {
		return promptservice.RuntimeDescriptor{Ref: value(ref), Version: runtimeRevisionMapInt64(data, version), Digest: strings.TrimPrefix(value(digest), "sha256:")}
	}
	result["INSTRUCTIONS"] = append(result["INSTRUCTIONS"], tuple("promptTemplateRef", "", "promptTemplateDigest"))
	if value("promptServiceTemplateDigest") != "" {
		result["INSTRUCTIONS"] = append(result["INSTRUCTIONS"], tuple("promptServiceTemplateRevision", "", "promptServiceTemplateDigest"))
	}
	result["MODEL"] = []promptservice.RuntimeDescriptor{{Value: value("runtimeProvider")}, {Value: value("runtimeModel")}}
	result["REASONING"] = []promptservice.RuntimeDescriptor{{Value: value("reasoningMode")}, {Value: value("effectiveReasoningEffort")}}
	result["IMAGE"] = []promptservice.RuntimeDescriptor{{Digest: strings.TrimPrefix(value("imageManifestDigest"), "sha256:")}}
	result["ENVIRONMENT"] = []promptservice.RuntimeDescriptor{tuple("runtimeEnvironmentRef", "runtimeEnvironmentVersion", "runtimeEnvironmentDigest"), tuple("environmentBindingRef", "environmentBindingVersion", "environmentBindingDigest")}
	result["POLICY"] = []promptservice.RuntimeDescriptor{tuple("runtimeConfigRef", "runtimeConfigVersion", "runtimeConfigDigest"), tuple("providerPolicyRef", "providerPolicyVersion", "providerPolicyDigest"), tuple("configOverlayRef", "configOverlayVersion", "configOverlayDigest")}
	for _, item := range continuationObjects(data["artifacts"]) {
		result["FILES"] = append(result["FILES"], promptservice.RuntimeDescriptor{Ref: continuationString(item, "ref"), Version: continuationNumber(item["revision"]), Digest: strings.TrimPrefix(continuationString(item, "digest"), "sha256:")})
	}
	context, _ := data["contextSnapshot"].(map[string]any)
	for _, scope := range []struct{ source, component, ref string }{{"skills", "SKILLS", "revision_ref"}, {"memories", "MEMORY", "revision_ref"}} {
		for _, item := range continuationObjects(context[scope.source]) {
			result[scope.component] = append(result[scope.component], promptservice.RuntimeDescriptor{Ref: continuationString(item, scope.ref), Version: continuationNumber(item["revision"]), Digest: continuationString(item, "digest")})
		}
	}
	for _, item := range continuationObjects(data["environmentTools"]) {
		result["TOOLS"] = append(result["TOOLS"], promptservice.RuntimeDescriptor{Value: continuationString(item, "name"), Digest: continuationDigest(item)})
	}
	for _, item := range continuationObjects(data["integrationGrants"]) {
		// В diff попадают exact refs и capability; credential и transport fields
		// не входят ни в отображение, ни в digest безопасного descriptor.
		safe := map[string]any{}
		for _, name := range []string{"ref", "grantVersion", "connectionRef", "connectionVersion", "definitionKey", "definitionVersion", "definitionDigest", "capabilityKey", "inputSchemaSha256"} {
			safe[name] = item[name]
		}
		result["INTEGRATIONS"] = append(result["INTEGRATIONS"], promptservice.RuntimeDescriptor{Ref: continuationString(item, "ref"), Version: continuationNumber(item["grantVersion"]), Value: continuationString(item, "capabilityKey"), Digest: continuationDigest(safe)})
	}
	for _, capability := range runtimeRevisionStringSlice(data["capabilities"]) {
		result["CAPABILITIES"] = append(result["CAPABILITIES"], promptservice.RuntimeDescriptor{Value: capability})
	}
	for name := range result {
		sort.Slice(result[name], func(i, j int) bool {
			a, b := result[name][i], result[name][j]
			return a.Ref+"\x00"+a.Value+"\x00"+a.Digest < b.Ref+"\x00"+b.Value+"\x00"+b.Digest
		})
	}
	// Полный MCP proof зависит от lease и не раскрывается. Этот descriptor
	// связывает доступные MCP действия, а сообщение — новый RuntimeRevision.
	result["MCP"] = []promptservice.RuntimeDescriptor{{Ref: "platform-mcp", Digest: continuationDigest(struct {
		Capabilities, Integrations []promptservice.RuntimeDescriptor
	}{result["CAPABILITIES"], result["INTEGRATIONS"]})}}
	return result, nil
}

func continuationObjects(value any) []map[string]any {
	items, _ := value.([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			result = append(result, object)
		}
	}
	return result
}
func continuationString(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return result
}
func continuationNumber(value any) int64 {
	switch number := value.(type) {
	case float64:
		return int64(number)
	case string:
		result, _ := strconv.ParseInt(number, 10, 64)
		return result
	}
	return 0
}
func continuationDigest(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

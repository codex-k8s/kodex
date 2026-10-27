package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryPromptContinuationPrevious Reads the latest stored runtime revision of the exact
// organization/session, ordered by creation/ref, for continuation comparison. It does not itself
// prove session authority or authorize a retry.
// queryPromptContinuationPrevious Читает последнюю сохранённую runtime-ревизию точных организации
// и сессии по времени и ссылке для сравнения продолжения. Самостоятельно не подтверждает
// полномочия сессии и не разрешает retry.
//
//go:embed sql/prompt_continuation_previous.sql
var queryPromptContinuationPrevious string

// queryPromptContinuationTemplate Reads the exact agent continuation-template binding and pinned
// published/superseded revision within matching organization/project/set. Absence preserves the
// built-in template; caller verifies bytes and digest before materialization.
// queryPromptContinuationTemplate Читает точную привязку continuation-шаблона агента и
// закреплённую опубликованную либо замещённую ревизию совпавших организации, проекта и набора.
// Отсутствие сохраняет встроенный шаблон; вызывающий проверяет байты и хеш до материализации.
//
//go:embed sql/prompt_continuation_template.sql
var queryPromptContinuationTemplate string

// queryPromptContinuationInsert Persists immutable continuation content/snapshot and
// template/service/variable/diff/materialization pins only between same-organization/session
// previous/current runtime revisions. Callers require one row in the owning execution transaction.
// queryPromptContinuationInsert Сохраняет неизменяемые содержимое, снимок и привязки шаблона,
// сервиса, переменных, diff и материализации только между прежней и текущей runtime-ревизиями
// одной организации и сессии. Вызывающий требует одну строку в транзакции выполнения владельца.
//
//go:embed sql/prompt_continuation_insert.sql
var queryPromptContinuationInsert string

// defaultContinuationTemplateRef Identifies the built-in continuation template when no
// owner-pinned agent template exists; changing this constant would alter provenance and is outside
// this annotation/refactoring.
// defaultContinuationTemplateRef Идентифицирует встроенный шаблон продолжения при отсутствии
// закреплённого шаблона владельца. Изменение константы затронуло бы происхождение и в текущий
// рефакторинг не входит.
const defaultContinuationTemplateRef = "prompt_continuation_default_v1"

// defaultContinuationTemplate Provides the exact existing runtime notice text with RUNTIME_CHANGES
// and INPUT slots; its bytes are hashed and retained unchanged rather than rewriting historical
// notices.
// defaultContinuationTemplate Содержит точный прежний текст runtime-уведомления со слотами
// RUNTIME_CHANGES и INPUT. Байты хешируются и сохраняются без изменения исторических уведомлений.
const defaultContinuationTemplate = "Continue the Session with the current approved runtime context.\n{{slot \"RUNTIME_CHANGES\"}}\n{{slot \"INPUT\"}}"

// preparedContinuationNotice Carries the prior internal runtime revision ID, safe component diff,
// exact prompt snapshot and complete materialization pending insertion in the caller transaction.
// Preparation does not grant execution authority or persist on its own.
// preparedContinuationNotice Хранит внутренний ID прежней runtime-ревизии, безопасный diff
// компонентов, точный снимок prompt и полную материализацию до вставки транзакцией вызывающего.
// Подготовка сама не выдаёт полномочия и ничего не сохраняет.
type preparedContinuationNotice struct {
	PreviousID      string
	Diff            promptservice.RuntimeDiff
	Snapshot        entity.PromptMaterializationSnapshot
	Materialization promptservice.Materialization
}

// prepareRuntimeContinuationNotice Reads previous/current dependency descriptors, builds the
// runtime diff and a copied owner/default-template snapshot, verifies template bytes/digest, binds
// canonical instructions/workflow/task/core projections and requires complete bounded
// materialization plus consumer-valid session context before adding notice metadata to the current
// snapshot. No previous revision returns nil,nil; failures retain stage-specific diagnostics.
// prepareRuntimeContinuationNotice Читает прежние и текущие дескрипторы, строит diff и копию
// снимка закреплённого либо встроенного шаблона, проверяет байты и хеш. Связывает канонические
// instructions, workflow, task и core, требует полную ограниченную материализацию и пригодный
// consumer-контекст до добавления метаданных в текущий снимок. Нет прежней ревизии — nil,nil;
// ошибки сохраняют диагностику этапа.
func (repository *Repository) prepareRuntimeContinuationNotice(ctx context.Context, tx pgx.Tx, current scope,
	snapshot map[string]any, promptSnapshot entity.PromptMaterializationSnapshot,
) (*preparedContinuationNotice, error) {
	var previousID, previousRef string
	var rawPrevious []byte
	if err := tx.QueryRow(ctx, queryPromptContinuationPrevious, pgx.StrictNamedArgs{"organization_id": current.organizationID, "session_ref": stringMap(snapshot, "sessionRef")}).Scan(&previousID, &previousRef, &rawPrevious); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Retry до первого успешного claim не имеет предыдущего runtime snapshot.
			return nil, nil
		}
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationPreviousRead, errs.ErrUnavailable)
	}
	var previous map[string]any
	if json.Unmarshal(rawPrevious, &previous) != nil {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationPreviousJson, errs.ErrConflict)
	}
	prior, err := continuationComponents(previous)
	if err != nil {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationPreviousDependencies, err)
	}
	next, err := continuationComponents(snapshot)
	if err != nil {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationCurrentDependencies, err)
	}
	diff, err := promptservice.CompareRuntimeContexts(prior, next, promptservice.RuntimeDiff{PreviousRevisionRef: previousRef,
		CurrentRevisionRef: stringMap(snapshot, "runtimeRevisionRef"), SessionRef: stringMap(snapshot, "sessionRef"), TurnRef: stringMap(snapshot, "turnRef"), Attempt: int32(runtimeRevisionMapInt64(snapshot, "attempt"))})
	if err != nil {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationDiff, errs.ErrConflict)
	}
	var noticeSnapshot entity.PromptMaterializationSnapshot
	raw, err := json.Marshal(promptSnapshot)
	if err != nil || json.Unmarshal(raw, &noticeSnapshot) != nil {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationSnapshot, errs.ErrConflict)
	}
	content := defaultContinuationTemplate
	noticeSnapshot.TemplateRef = defaultContinuationTemplateRef
	digest := sha256.Sum256([]byte(content))
	noticeSnapshot.TemplateDigest = hex.EncodeToString(digest[:])
	err = tx.QueryRow(ctx, queryPromptContinuationTemplate, pgx.StrictNamedArgs{"organization_id": current.organizationID, "agent_ref": stringMap(snapshot, "agentRef")}).Scan(&noticeSnapshot.TemplateRef, &noticeSnapshot.TemplateDigest, &content)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationTemplateRead, errs.ErrUnavailable)
	}
	if len(content) > 16<<10 {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationTemplateBytes, errs.ErrConflict)
	}
	digest = sha256.Sum256([]byte(content))
	if noticeSnapshot.TemplateDigest != hex.EncodeToString(digest[:]) {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationTemplateDigest, errs.ErrConflict)
	}
	rawDiff, err := json.Marshal(diff)
	if err != nil {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationDiffJson, errs.ErrConflict)
	}
	noticeSnapshot.TemplateContent = content
	noticeSnapshot.ServiceTemplateRevision = promptservice.ServiceTemplateRevision
	noticeSnapshot.TargetKind, noticeSnapshot.TargetRef = promptservice.TargetSessionContinuation, stringMap(snapshot, "sessionRef")
	noticeSnapshot.ExtraTemplates = nil
	noticeSnapshot.StagePurposeTemplate, noticeSnapshot.StageExpectedResultTemplate = "", ""
	noticeSnapshot.ContextPin.PreviousRuntimeRevisionRef = previousRef
	noticeSnapshot.SessionContinuation = string(rawDiff)
	noticeSnapshot.SemanticValues = nil
	noticeSnapshot.PlatformInstructions = nil
	noticeSnapshot.UnavailableVariables = nil
	if err := bindPlatformInstructions(&noticeSnapshot); err != nil {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationBinding, err)
	}
	if err := repository.hydrateWorkflowExecutionPromptTx(ctx, tx, current, stringMap(snapshot, "nodeRef"), &noticeSnapshot); err != nil {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationWorkflowContext, err)
	}
	if err := bindRuntimeContinuationTaskProjection(&noticeSnapshot, snapshot); err != nil {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationTaskProjection, err)
	}
	if err := bindRuntimeContinuationCoreProjection(&noticeSnapshot, promptSnapshot, snapshot); err != nil {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationBinding, err)
	}
	materialized, err := promptservice.Materialize(content, promptservice.FromSnapshot(noticeSnapshot))
	if err != nil {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationMaterialization, errs.ErrConflict)
	}
	if !materialized.Complete {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationIncomplete, errs.ErrConflict)
	}
	if len(materialized.Prompt) > runtimecontract.MaximumContinuationNoticeBytes {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationNoticeBytes, errs.ErrConflict)
	}
	if err := appendRuntimeContinuationContext(snapshot, materialized); err != nil {
		return nil, runtimeEligibilityDiagnostic(runtimeReasonContinuationSessionContext, err)
	}
	snapshot["continuationPromptSnapshot"], snapshot["continuationRuntimeDiff"], snapshot["continuationNoticeDigest"] = noticeSnapshot, diff, materialized.Digest
	return &preparedContinuationNotice{PreviousID: previousID, Diff: diff, Snapshot: noticeSnapshot, Materialization: materialized}, nil
}

// bindRuntimeContinuationCoreProjection Only for a PROJECT assistant core, verifies the exact
// current main prompt/snapshot/core/run/session/node/turn/attempt pins, then adds a compact
// canonical JSON core projection to the supplementary notice. Rejects an existing marker; main
// instructions and immutable history stay unchanged and full instructions are delivered
// separately.
// bindRuntimeContinuationCoreProjection Только для PROJECT core сверяет точные текущие main
// prompt, снимок, core, run, session, node, turn и attempt, затем добавляет компактную
// каноническую JSON-проекцию core в дополнительное уведомление. Отклоняет прежний маркер; основные
// инструкции и история не меняются, полные инструкции передаются отдельно.
func bindRuntimeContinuationCoreProjection(notice *entity.PromptMaterializationSnapshot, source entity.PromptMaterializationSnapshot, current map[string]any) error {
	if source.AssistantCore == nil || source.AssistantCore.Scope != "PROJECT" {
		return nil
	}
	if notice.TargetKind != promptservice.TargetSessionContinuation || notice.TargetRef != source.SessionRef ||
		!reflect.DeepEqual(notice.AssistantCore, source.AssistantCore) || notice.ProjectRef != source.ProjectRef ||
		notice.ContextPin.AgentRef != source.ContextPin.AgentRef {
		return errs.ErrConflict
	}
	main, err := promptservice.Materialize(source.TemplateContent, promptservice.FromSnapshot(source))
	if err != nil || !main.Complete || main.Prompt != stringMap(current, "instructions") ||
		main.Digest != stringMap(current, "promptMaterializationDigest") ||
		source.ContextPin.AgentRef != stringMap(current, "agentRef") || source.ProjectRef != stringMap(current, "projectRef") ||
		source.RunRef != stringMap(current, "runRef") || source.SessionRef != stringMap(current, "sessionRef") ||
		source.Variables["node.ref"] != stringMap(current, "nodeRef") || source.Variables["turn.ref"] != stringMap(current, "turnRef") ||
		runtimeRevisionMapInt64(current, "attempt") < 1 || runtimeRevisionMapInt64(current, "attempt") > math.MaxInt32 {
		return errs.ErrConflict
	}
	digest := sha256.Sum256([]byte(main.Prompt))
	if hex.EncodeToString(digest[:]) != stringMap(current, "instructionDigest") {
		return errs.ErrConflict
	}
	sourceRaw, err := json.Marshal(source)
	currentRaw, currentErr := json.Marshal(current["promptSnapshot"])
	var sourceObject, currentObject map[string]any
	if err != nil || currentErr != nil || json.Unmarshal(sourceRaw, &sourceObject) != nil ||
		json.Unmarshal(currentRaw, &currentObject) != nil || !reflect.DeepEqual(sourceObject, currentObject) {
		return errs.ErrConflict
	}
	runtime, ok := notice.StructuredVariables["runtime"].(map[string]any)
	if !ok && notice.StructuredVariables["runtime"] != nil {
		return errs.ErrConflict
	}
	if _, present := runtime[promptservice.AssistantCoreContinuationProjectionKey]; present {
		return errs.ErrConflict
	}
	if runtime == nil {
		runtime = make(map[string]any)
	}
	core := source.AssistantCore
	projection := promptservice.AssistantCoreContinuationProjection{
		Revision:  promptservice.AssistantCoreContinuationProjectionRevision,
		CoreScope: core.Scope, CoreRef: core.Ref, CoreRevision: core.Revision, CoreDigest: core.Digest,
		InstructionsSHA256: hex.EncodeToString(digest[:]), InstructionsBytes: len(main.Prompt), PromptMaterializationDigest: main.Digest,
		RuntimeRevisionRef: stringMap(current, "runtimeRevisionRef"), AgentRef: source.ContextPin.AgentRef, ProjectRef: source.ProjectRef,
		RunRef: source.RunRef, NodeRef: source.Variables["node.ref"], SessionRef: source.SessionRef, TurnRef: source.Variables["turn.ref"],
		Attempt: int32(runtimeRevisionMapInt64(current, "attempt")),
	}
	// Object имеет одинаковый canonical JSON до seal и после durable readback;
	// typed struct внутри any дал бы другой порядок keys на rejoin.
	projectionRaw, err := json.Marshal(projection)
	var projectionObject map[string]any
	if err != nil || json.Unmarshal(projectionRaw, &projectionObject) != nil {
		return errs.ErrConflict
	}
	runtime[promptservice.AssistantCoreContinuationProjectionKey] = projectionObject
	notice.StructuredVariables["runtime"] = runtime
	return nil
}

// appendRuntimeContinuationContext Copies the session-context slice and appends one full server
// notice as USER only below 128 messages, then applies the same runtime consumer predicate before
// replacing the snapshot field. It neither truncates historical/user content nor treats ordinary
// text as a semantic notice.
// appendRuntimeContinuationContext Копирует срез контекста и добавляет одно полное серверное
// уведомление USER только при менее 128 сообщениях, затем применяет тот же runtime
// consumer-предикат до замены поля снимка. Историю и пользовательский текст не усекает, обычный
// текст семантическим уведомлением не объявляет.
func appendRuntimeContinuationContext(snapshot map[string]any, materialized promptservice.Materialization) error {
	messages, ok := snapshot["sessionContext"].([]map[string]string)
	if !ok && snapshot["sessionContext"] != nil {
		return errs.ErrConflict
	}
	if len(messages) >= 128 {
		return errs.ErrConflict
	}
	candidate := append(append([]map[string]string{}, messages...), map[string]string{"role": "USER", "content": materialized.Prompt})
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn,
		RuntimeRevisionRef: stringMap(snapshot, "runtimeRevisionRef"), SessionRef: stringMap(snapshot, "sessionRef"),
		TurnRef: stringMap(snapshot, "turnRef"), Attempt: int32(runtimeRevisionMapInt64(snapshot, "attempt")),
		Capabilities:                  runtimeRevisionStringSlice(snapshot["capabilities"]),
		PromptServiceTemplateRevision: stringMap(snapshot, "promptServiceTemplateRevision"),
		SessionContext:                runtimeRevisionSessionContext(candidate),
	}
	if notice, err := runtimecontract.ValidateRuntimeSessionContext(input); err != nil || !notice {
		return errs.ErrConflict
	}
	snapshot["sessionContext"] = candidate
	return nil
}

// persistRuntimeContinuationNotice Skips nil notices, marshals the exact prepared snapshot and
// inserts content plus all provenance pins between prior/current owner revisions, requiring one
// affected row. Any marshal/write/count failure is conflict; commit belongs to the caller.
// persistRuntimeContinuationNotice Пропускает nil-уведомление, кодирует точный подготовленный
// снимок и вставляет содержимое со всеми привязками происхождения между ревизиями владельца,
// требуя одну строку. Ошибка кодирования, записи или числа строк даёт Conflict; commit принадлежит
// вызывающему.
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

// continuationComponents Normalizes a snapshot through JSON and builds sorted closed runtime
// component descriptors, binding platform
// instructions/core/templates/model/policy/files/context/tools and safe integration fields.
// Credential/transport values are excluded from integration descriptors; MCP digest binds the safe
// capability/integration projection. This is change explanation, not a new grant.
// continuationComponents Нормализует снимок через JSON и строит отсортированные дескрипторы
// закрытого набора runtime-компонентов: инструкции, core, шаблоны, модель, policy, файлы,
// контекст, инструменты и безопасные поля интеграций. Credential и transport значения в
// интеграционные дескрипторы не входят; MCP-хеш связывает безопасные возможности и интеграции. Это
// объяснение изменений, а не новый grant.
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
	promptSnapshot, _ := data["promptSnapshot"].(map[string]any)
	if raw, present := data["promptSnapshot"]; present {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, errs.ErrConflict
		}
		instruction, err := platformInstructionDependency(encoded)
		if err != nil {
			return nil, err
		}
		if instruction != nil {
			result["INSTRUCTIONS"] = append(result["INSTRUCTIONS"], *instruction)
		}
	}
	if core, present := promptSnapshot["assistantCore"]; present {
		binding, ok := core.(map[string]any)
		if !ok || continuationString(binding, "Ref") == "" || continuationString(binding, "Revision") == "" || continuationString(binding, "Digest") == "" {
			return nil, errs.ErrConflict
		}
		result["INSTRUCTIONS"] = append(result["INSTRUCTIONS"], promptservice.RuntimeDescriptor{
			Ref: continuationString(binding, "Ref"), Digest: continuationString(binding, "Digest"), Value: continuationString(binding, "Revision"),
		})
	}
	for _, extra := range continuationObjects(promptSnapshot["extraTemplates"]) {
		result["INSTRUCTIONS"] = append(result["INSTRUCTIONS"], promptservice.RuntimeDescriptor{Ref: continuationString(extra, "Ref"), Digest: continuationString(extra, "Digest"), Value: continuationString(extra, "Kind")})
	}
	for _, field := range []string{"stagePurposeTemplate", "stageExpectedResultTemplate"} {
		if text := continuationString(promptSnapshot, field); text != "" {
			result["INSTRUCTIONS"] = append(result["INSTRUCTIONS"], promptservice.RuntimeDescriptor{Ref: field, Digest: continuationDigest(text)})
		}
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
		for _, name := range []string{"ref", "grantVersion", "approvalPolicy", "connectionRef", "connectionVersion", "definitionKey", "definitionVersion", "definitionDigest", "capabilityKey", "inputSchemaSha256"} {
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

// continuationObjects Extracts object elements only from a []any input into an allocated ordered
// slice, silently skipping nonobjects and treating other container types as empty. This
// compatibility accessor performs no schema/authority validation.
// continuationObjects Извлекает только объекты из []any в созданный упорядоченный срез, пропуская
// прочие элементы и считая иной тип контейнера пустым. Доступ совместимости не проверяет схему или
// полномочия.
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

// continuationString Returns a string-valued map entry or empty string for missing/wrong-type
// input without coercion. Required-field checks belong to descriptor builders.
// continuationString Возвращает строковое поле карты либо пустую строку при отсутствии или ином
// типе без приведения. Обязательные поля проверяются построителями дескрипторов.
func continuationString(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return result
}

// continuationNumber Accepts nil as zero, signed integer types directly, nonnegative finite
// integral JSON-safe float64, or canonical signed decimal int64 strings; unsupported/malformed
// values return -1. The integer/string paths deliberately retain their historical signed behavior.
// continuationNumber Принимает nil как ноль, знаковые целые напрямую, неотрицательный конечный
// целый float64 в безопасном JSON-диапазоне либо каноническую знаковую десятичную строку int64.
// Иной или неверный вход даёт -1; целые и строки сохраняют прежнее знаковое поведение.
func continuationNumber(value any) int64 {
	switch number := value.(type) {
	case nil:
		return 0
	case int:
		return int64(number)
	case int32:
		return int64(number)
	case int64:
		return number
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < 0 || number > 9007199254740991 {
			return -1
		}
		return int64(number)
	case string:
		result, err := strconv.ParseInt(number, 10, 64)
		if err != nil || strconv.FormatInt(result, 10) != number {
			return -1
		}
		return result
	}
	return -1
}

// continuationDigest Returns lowercase SHA-256 of the existing JSON encoding or empty string on
// marshal failure. It is a safe descriptor commitment, not canonical JCS, authentication or
// permission to execute.
// continuationDigest Возвращает SHA-256 в нижнем hex-регистре от прежнего JSON-кодирования либо
// пустую строку при ошибке marshal. Это фиксация дескриптора, а не JCS, аутентификация или
// разрешение выполнения.
func continuationDigest(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

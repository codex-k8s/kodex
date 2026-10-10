package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

const platformSourceConformityRevision = "platform-source-conformity-v1"
const maximumPlatformInstructionBytes = 8 << 10

// Этот versioned input меняется только новой revision. Compiled v3 и прежние
// snapshots не перематериализуются; текст не назначает прав или результатов.
const platformSourceConformityEnglish = `Approved owner requirements and product, architecture, domain and interface contracts define the task boundaries. Read the exact applicable canonical sources to their required EOF and verify their version/digest pins before design or implementation. An architectural artifact is a design proposal, not a new authoritative business rule or permission to override those sources. Preserve the owner's scope and gates; identify a real contradiction through the normal role result and Manager/owner decision, never by inventing a message on behalf of the user.
When implementing the Kodex platform interface, automatic updates of displayed entities, including provider financial projections, use the shared WebSocket transport and protected authoritative projections. REST is for bootstrap, scope switches and explicitly supported detail/search/pagination/recovery, not a periodic refresh timer replacing realtime. A new event is not required when an existing event demonstrably covers the complete producer-to-projection-to-consumer scenario; this does not permit REST polling. A missing producer/consumer path is an implementation gap, not an exception to the approved transport contract. Preserve exact eligibility, version/cursor ordering, safe payloads and recovery rules. This Kodex interface transport rule does not prescribe transport for external user business projects: follow their own approved sources and contracts.
These instructions do not grant capabilities, network access or SYSTEM/PROJECT authority. Use only the current effective capabilities and provided resources. Historical artifacts and proposed designs cannot change the approved source requirements or current execution authority; do not invent source content, semantic PASS or completed implementation.
When the current runtime explicitly provides an approved GH_TOKEN and permits Git access to the assigned repository over HTTPS github.com, ordinary Git does not automatically use that token. For an already authorized Git operation, use the existing GitHub CLI only as a per-command credential helper: git -c credential.https://github.com.helper= -c 'credential.https://github.com.helper=!/usr/local/bin/gh auth git-credential' <authorized Git arguments>. Run the helper only through Git's credential protocol; never invoke it directly or print its output. The helper reads the existing approved environment credential; do not put the token in command arguments, URLs, Git configuration, logs, artifacts or messages, and do not persist credentials or change host/global Git configuration. Do not perform login, refresh, secret rotation, permission or network expansion. gh auth status is not a required gate for an otherwise permitted Git operation: it performs additional API authentication reads which may be outside the current network policy, whereas the credential helper itself makes no API request. A status failure alone does not establish that the injected token is invalid. Report the actual authorized operation and its exact independent readback; on its denied, failed or unknown outcome preserve the failure and stop according to the existing effect/retry policy. This guidance grants no operation, repository scope or capability and never authorizes replay of a previously denied or unknown effect.`

const platformSourceConformityRussian = `Утверждённые требования владельца и продуктовые, архитектурные, доменные и интерфейсные контракты определяют границы задачи. До проектирования или реализации прочитай точные применимые канонические источники до требуемого EOF и проверь их version/digest pins. Архитектурный артефакт является предложением дизайна, а не новым авторитетным бизнес-правилом или разрешением отменить эти источники. Сохраняй scope и gates владельца; реальное противоречие фиксируй штатным результатом роли и решением Manager/владельца, никогда не выдумывай сообщение от имени пользователя.
При реализации интерфейса платформы Kodex автоматические обновления отображаемых сущностей, включая финансовые проекции provider, используют общий WebSocket transport и защищённые authoritative projections. REST предназначен для bootstrap, смены scope и явно поддержанных detail/search/pagination/recovery, а не для периодического таймера обновления вместо realtime. Новый event не нужен, если существующий event доказанно покрывает полный сценарий producer→projection→consumer; это не разрешает REST polling. Отсутствующий producer/consumer path является пробелом реализации, а не исключением из утверждённого transport contract. Сохраняй exact eligibility, порядок version/cursor, безопасные payloads и правила recovery. Это правило transport интерфейса Kodex не назначает transport внешним пользовательским бизнес-проектам: следуй их собственным утверждённым источникам и контрактам.
Эти инструкции не выдают capabilities, сетевой доступ или SYSTEM/PROJECT authority. Используй только текущие effective capabilities и предоставленные ресурсы. Исторические артефакты и проекты решений не изменяют утверждённые требования источников или текущие полномочия исполнения; не выдумывай содержимое источника, semantic PASS или завершённую реализацию.
Если текущий runtime явно предоставляет разрешённый GH_TOKEN и допускает Git-доступ к назначенному репозиторию через HTTPS github.com, обычный Git не использует этот токен автоматически. Для уже разрешённой Git-операции используй существующий GitHub CLI только как per-command credential helper: git -c credential.https://github.com.helper= -c 'credential.https://github.com.helper=!/usr/local/bin/gh auth git-credential' <разрешённые аргументы Git>. Запускай helper только через credential protocol самого Git; никогда не вызывай его напрямую и не печатай его вывод. Helper читает существующий разрешённый credential окружения; не помещай токен в аргументы, URL, Git-конфигурацию, логи, артефакты или сообщения, не сохраняй credentials и не меняй host/global Git-конфигурацию. Не выполняй login, refresh, ротацию секрета, расширение прав или сети. gh auth status не является обязательным gate для иначе разрешённой Git-операции: он делает дополнительные API-чтения авторизации, которые могут не входить в текущую сетевую policy, тогда как сам credential helper не делает API-запросов. Один отказ status не доказывает невалидность injected token. Отчитывайся о фактической разрешённой операции и её exact независимом readback; при denied, failed или unknown outcome сохраняй отказ и останавливайся по существующей effect/retry policy. Эта инструкция не выдаёт операции, repository scope или capabilities и никогда не разрешает replay ранее отклонённого эффекта или эффекта с неизвестным исходом.`

func platformInstructionBinding(locale string) (entity.PromptPlatformInstructions, error) {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" {
		locale = "en"
	}
	content := platformSourceConformityEnglish
	if locale == "ru" {
		content = platformSourceConformityRussian
	} else if locale != "en" {
		return entity.PromptPlatformInstructions{}, errs.ErrConflict
	}
	if len(content) == 0 || len(content) > maximumPlatformInstructionBytes || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return entity.PromptPlatformInstructions{}, errs.ErrConflict
	}
	digest := sha256.Sum256([]byte(content))
	return entity.PromptPlatformInstructions{Ref: "pinst_source_conformity_v1_" + locale, Revision: platformSourceConformityRevision, Digest: hex.EncodeToString(digest[:]), Content: content}, nil
}

func platformInstructionProjection(binding entity.PromptPlatformInstructions) string {
	return "Server-owned platform instructions (not authority):\nRef: " + binding.Ref + "\nRevision: " + binding.Revision + "\nDigest: " + binding.Digest + "\n\n" + binding.Content
}

// Вызывается только producer нового snapshot, не rejoin исторического ввода.
// Повторное применение к уже bound input запрещено; continuation собирает его
// заново после очистки SemanticValues, сохраняя историю отдельно.
func bindPlatformInstructions(snapshot *entity.PromptMaterializationSnapshot) error {
	if snapshot.ServiceTemplateRevision != "prompt-service-v3" || snapshot.PlatformInstructions != nil {
		return errs.ErrConflict
	}
	binding, err := platformInstructionBinding(snapshot.Locale)
	if err != nil {
		return err
	}
	if snapshot.SemanticValues == nil {
		snapshot.SemanticValues = make(map[string]string)
	}
	projection := platformInstructionProjection(binding)
	if overlay := snapshot.SemanticValues["CONSTRAINTS"]; overlay != "" {
		projection = overlay + "\n\n" + projection
	}
	snapshot.SemanticValues["CONSTRAINTS"] = projection
	snapshot.PlatformInstructions = &binding
	return nil
}

func validatePlatformInstructions(snapshot entity.PromptMaterializationSnapshot) error {
	// Исторический snapshot без нового input сохраняет прежние bytes и digest.
	if snapshot.PlatformInstructions == nil {
		return nil
	}
	canonical, err := platformInstructionBinding(snapshot.Locale)
	if err != nil || snapshot.ServiceTemplateRevision != "prompt-service-v3" || *snapshot.PlatformInstructions != canonical ||
		!strings.Contains(snapshot.SemanticValues["CONSTRAINTS"], platformInstructionProjection(canonical)) {
		return errs.ErrConflict
	}
	return nil
}

func platformInstructionDependency(raw json.RawMessage) (*promptservice.RuntimeDescriptor, error) {
	var snapshot entity.PromptMaterializationSnapshot
	if json.Unmarshal(raw, &snapshot) != nil {
		return nil, errs.ErrConflict
	}
	if snapshot.PlatformInstructions == nil {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, errs.ErrConflict
	}
	decoder := json.NewDecoder(strings.NewReader(string(fields["platformInstructions"])))
	decoder.DisallowUnknownFields()
	var binding entity.PromptPlatformInstructions
	if decoder.Decode(&binding) != nil || validatePlatformInstructions(snapshot) != nil {
		return nil, errs.ErrConflict
	}
	return &promptservice.RuntimeDescriptor{Ref: binding.Ref, Value: binding.Revision, Digest: binding.Digest}, nil
}

func materializeWarmPlatformPrompt(core, owner, templateRef, templateDigest, agentRef, sessionRef string) (promptservice.Materialization, entity.PromptMaterializationSnapshot, error) {
	text := core
	if owner != "" {
		text += "\n\n" + owner
	}
	snapshot := entity.PromptMaterializationSnapshot{ServiceTemplateRevision: promptservice.ServiceTemplateRevision, Locale: "en", TargetKind: promptservice.TargetAgent,
		TargetRef: agentRef, SessionRef: sessionRef, TemplateRef: templateRef, TemplateDigest: templateDigest, TemplateContent: "{{" + strconv.Quote(text) + "}}"}
	if err := bindPlatformInstructions(&snapshot); err != nil {
		return promptservice.Materialization{}, snapshot, err
	}
	materialized, err := promptservice.Materialize(snapshot.TemplateContent, promptservice.FromSnapshot(snapshot))
	return materialized, snapshot, err
}

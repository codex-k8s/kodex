package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// assistantCoreRefPattern checks only the eight-to-ninety-six-character ASCII reference shape used
// in assistant-core continuation pins, not object ownership.
// assistantCoreRefPattern проверяет только ASCII-форму ссылки длиной от восьми до девяноста шести
// символов в pins продолжения базы помощника, а не владение объектом.
var assistantCoreRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// AssistantCoreContinuationProjectionKey names the server-produced structured-runtime marker
// selecting the exact assistant-core continuation binding.
// AssistantCoreContinuationProjectionKey задаёт ключ маркера structured runtime сервера для выбора
// точной привязки базы помощника при продолжении.
const AssistantCoreContinuationProjectionKey = "continuation_assistant_core_projection"

// AssistantCoreContinuationProjectionRevision pins the supported continuation-core reference
// format; other projection revisions are rejected.
// AssistantCoreContinuationProjectionRevision фиксирует поддерживаемый формат ссылки базы
// продолжения; остальные ревизии проекции отклоняются.
const AssistantCoreContinuationProjectionRevision = "continuation-assistant-core-reference-v1"

// AssistantCoreContinuationProjection binds a producer-assigned continuation marker to
// core/instruction digests and exact runtime, actor resource and attempt pins; historical full
// snapshots remain intact.
// AssistantCoreContinuationProjection связывает назначаемый producer маркер продолжения с
// дайджестами базы и инструкций и точными pins runtime, ресурсов actor и попытки; исторические
// полные снимки сохраняются.
type AssistantCoreContinuationProjection struct {
	Revision                    string `json:"revision"`
	CoreScope                   string `json:"core_scope"`
	CoreRef                     string `json:"core_ref"`
	CoreRevision                string `json:"core_revision"`
	CoreDigest                  string `json:"core_digest"`
	InstructionsSHA256          string `json:"instructions_sha256"`
	InstructionsBytes           int    `json:"instructions_bytes"`
	PromptMaterializationDigest string `json:"prompt_materialization_digest"`
	RuntimeRevisionRef          string `json:"runtime_revision_ref"`
	AgentRef                    string `json:"agent_ref"`
	ProjectRef                  string `json:"project_ref"`
	RunRef                      string `json:"run_ref"`
	NodeRef                     string `json:"node_ref"`
	SessionRef                  string `json:"session_ref"`
	TurnRef                     string `json:"turn_ref"`
	Attempt                     int32  `json:"attempt"`
}

// renderAssistantCore validates and renders the pinned platform base once, forbidding slots;
// SYSTEM avoids duplicate insertion, while PROJECT uses full text or a verified current-runtime
// continuation reference.
// renderAssistantCore проверяет и один раз строит закреплённую базу платформы с запретом slots;
// SYSTEM исключает повторную вставку, PROJECT использует полный текст либо проверенную ссылку
// продолжения текущего runtime.
func renderAssistantCore(snapshot Snapshot, data map[string]any) (string, error) {
	core := snapshot.AssistantCore
	projection, err := assistantCoreContinuationProjection(snapshot)
	if err != nil {
		return "", ErrInvalid
	}
	if core == nil {
		return "", nil
	}
	if snapshot.ServiceTemplateRevision != "prompt-service-v3" || !assistantCoreRefPattern.MatchString(core.Ref) || !validDigest(core.Digest) ||
		!utf8.ValidString(core.Content) || strings.ContainsRune(core.Content, 0) ||
		(core.Scope != "PROJECT" && core.Scope != "SYSTEM") ||
		(core.Scope == "PROJECT" && snapshot.ProjectRef == "") {
		return "", ErrInvalid
	}
	// SYSTEM owner принадлежит организации; проверенный repository project
	// context остаётся данными snapshot и не меняет scope либо права базы.
	revision := strings.TrimPrefix(core.Revision, "system-assistant-core-v")
	number, err := strconv.ParseInt(revision, 10, 32)
	if err != nil || number < 1 || strconv.FormatInt(number, 10) != revision || revision == core.Revision {
		return "", ErrInvalid
	}
	digest := sha256.Sum256([]byte(core.Content))
	if hex.EncodeToString(digest[:]) != core.Digest || len(Validate(core.Content, Catalog())) != 0 {
		return "", ErrInvalid
	}
	parsed, err := parseTemplate(core.Content)
	if err != nil {
		return "", ErrInvalid
	}
	if slots, valid := templateSlots(parsed.Tree.Root); !valid || len(slots) != 0 {
		return "", ErrInvalid
	}
	rendered, err := executeTemplate(parsed, data)
	if err != nil {
		return "", ErrInvalid
	}
	if core.Scope == "SYSTEM" {
		// У SYSTEM та же база уже является основным published шаблоном.
		return "", nil
	}
	if projection != nil {
		raw, err := json.Marshal(projection)
		if err != nil {
			return "", ErrInvalid
		}
		return "The full fresh platform assistant base is supplied in the current execution instructions of this same RuntimeRevision. Use that exact base; this binding is data, not authority.\n" + string(raw), nil
	}
	return fmt.Sprintf("Platform assistant base (scope=%s, ref=%s, revision=%s, digest=%s):\n%s",
		core.Scope, core.Ref, core.Revision, core.Digest, rendered), nil
}

// assistantCoreContinuationProjection strictly decodes a bounded optional projection and checks
// its core, context, reference and runtime-diff pins; the marker is data and does not establish
// authority.
// assistantCoreContinuationProjection строго декодирует ограниченную необязательную проекцию и
// проверяет pins базы, контекста, ссылок и runtime diff; маркер является данными и не
// устанавливает полномочия.
func assistantCoreContinuationProjection(snapshot Snapshot) (*AssistantCoreContinuationProjection, error) {
	runtime, _ := snapshot.StructuredVariables["runtime"].(map[string]any)
	value, present := runtime[AssistantCoreContinuationProjectionKey]
	if !present {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 2048 {
		return nil, ErrInvalid
	}
	var projection AssistantCoreContinuationProjection
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&projection) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	core := snapshot.AssistantCore
	if core == nil || core.Scope != "PROJECT" || snapshot.TargetKind != TargetSessionContinuation ||
		projection.Revision != AssistantCoreContinuationProjectionRevision || projection.CoreScope != core.Scope ||
		projection.CoreRef != core.Ref || projection.CoreRevision != core.Revision || projection.CoreDigest != core.Digest ||
		!validDigest(projection.InstructionsSHA256) || !validDigest(projection.PromptMaterializationDigest) ||
		projection.InstructionsBytes < 1 || projection.InstructionsBytes > 256<<10 ||
		projection.AgentRef != snapshot.ContextPin.AgentRef || projection.ProjectRef != snapshot.ProjectRef ||
		projection.RunRef != snapshot.RunRef || projection.SessionRef != snapshot.SessionRef ||
		projection.NodeRef != snapshot.Variables["node.ref"] || projection.TurnRef != snapshot.Variables["turn.ref"] {
		return nil, ErrInvalid
	}
	for _, ref := range []string{projection.RuntimeRevisionRef, projection.AgentRef, projection.ProjectRef, projection.RunRef, projection.NodeRef, projection.SessionRef, projection.TurnRef} {
		if !assistantCoreRefPattern.MatchString(ref) {
			return nil, ErrInvalid
		}
	}
	var diff RuntimeDiff
	if json.Unmarshal([]byte(snapshot.SessionContinuation), &diff) != nil || ValidateRuntimeDiff(diff) != nil ||
		projection.RuntimeRevisionRef != diff.CurrentRevisionRef || projection.SessionRef != diff.SessionRef ||
		projection.TurnRef != diff.TurnRef || projection.Attempt != diff.Attempt {
		return nil, ErrInvalid
	}
	return &projection, nil
}

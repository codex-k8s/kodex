// Package workflow implements bounded workflow predicates independently of storage and transport.
// Package workflow реализует ограниченные условия процесса независимо от хранения и транспорта.
package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// ServerDAGV1 selects the explicit server-driven policy; absent mode retains historical semantics.
// ServerDAGV1 выбирает серверную политику; отсутствие режима сохраняет историческое поведение.
const ServerDAGV1 = "SERVER_DAG_V1"

// MaximumSteps bounds the first server profile while legacy versions retain their existing limits.
// MaximumSteps ограничивает первый серверный профиль, не изменяя пределы прежних версий.
const MaximumSteps = 12

// MaximumAttempts bounds completed business attempts of one logical server step.
// MaximumAttempts ограничивает завершённые бизнес-попытки одного серверного шага.
const MaximumAttempts = 4

// MaximumResultBytes caps a typed result declaration before parsing; evidence stays owner-resolved.
// MaximumResultBytes ограничивает объявление результата до разбора; evidence разрешает владелец.
const MaximumResultBytes = 8 << 10

var (
	// artifactRefPattern bounds a locator without granting access to the referenced artifact.
	// artifactRefPattern ограничивает ссылку, не выдавая доступа к указанному артефакту.
	artifactRefPattern = regexp.MustCompile(`^art_[A-Za-z0-9_-]{8,92}$`)
	// digestPattern requires the exact SHA256 representation returned by the immutable file catalog.
	// digestPattern требует точное представление SHA256 из неизменяемого файлового каталога.
	digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	// receiptRefPattern rejects ambiguous receipt locators before any owner lookup.
	// receiptRefPattern отклоняет неоднозначные ссылки до owner lookup.
	receiptRefPattern = regexp.MustCompile(`^erc_[A-Za-z0-9_-]{8,92}$`)
	// ErrResultInvalid reports a closed structural error without exposing model content.
	// ErrResultInvalid сообщает закрытую структурную ошибку без раскрытия ответа модели.
	ErrResultInvalid = errors.New("workflow result declaration invalid")
	// resultKey bounds names shared by graph steps and mandatory evidence predicates.
	// resultKey ограничивает имена шагов и обязательных доказательств.
	resultKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)
	// RepositoryPattern excludes URL, credentials and ambiguous repository selectors.
	// RepositoryPattern исключает URL, credentials и неоднозначные селекторы репозитория.
	RepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	// CommitPattern accepts exact Git commit identities, never branches or shortened hashes.
	// CommitPattern принимает точные Git commit identity, исключая ветки и короткие хеши.
	CommitPattern = regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`)
)

// ValidRequirements checks a closed bounded predicate registry and exact predecessor references.
// ValidRequirements проверяет закрытый ограниченный реестр и точные ссылки на предшественников.
func ValidRequirements(requirements []entity.WorkflowResultRequirement, dependencies []string) bool {
	if len(requirements) < 1 || len(requirements) > 4 {
		return false
	}
	seen := map[string]bool{}
	for _, r := range requirements {
		if len(r.Repository) > 200 || !resultKey.MatchString(r.Key) || seen[r.Key] || (r.Kind != "DEPLOYMENT" && r.TargetRef != "") {
			return false
		}
		seen[r.Key] = true
		switch r.Kind {
		case "ARTIFACT":
			if r.FileName == "" || r.FileName == "." || r.FileName == ".." || r.FileName == "result.md" || r.FileName == "workspace-write-result.json" || len(r.FileName) > 160 || strings.ContainsAny(r.FileName, "/\\\x00\r\n") || r.MediaType == "" || len(r.MediaType) > 160 || !utf8.ValidString(r.MediaType) || strings.ContainsAny(r.MediaType, "\x00\r\n") || !utf8.ValidString(r.FileName) || r.Repository != "" || r.SubjectStepKey != "" || r.Operation != "" {
				return false
			}
		case "PULL_REQUEST":
			if !RepositoryPattern.MatchString(r.Repository) || r.FileName != "" || r.MediaType != "" || r.SubjectStepKey != "" || r.Operation != "" {
				return false
			}
		case "REVIEW", "QA":
			found := false
			for _, key := range dependencies {
				found = found || key == r.SubjectStepKey
			}
			if !found || r.FileName != "" || r.MediaType != "" || r.Repository != "" || r.Operation != "" {
				return false
			}
		case "DEPLOYMENT":
			found := false
			for _, key := range dependencies {
				found = found || key == r.SubjectStepKey
			}
			if !found || !RepositoryPattern.MatchString(r.Repository) || r.Operation != "ssh.compose.deploy" || !strings.HasPrefix(r.TargetRef, "ssh_") || len(r.TargetRef) > 96 || r.FileName != "" || r.MediaType != "" {
				return false
			}
		case "INTEGRATION_RECEIPT":
			if !RepositoryPattern.MatchString(r.Repository) || r.Operation != "github.pull_request.merge" || r.FileName != "" || r.MediaType != "" || r.SubjectStepKey == "" {
				return false
			}
			found := false
			for _, key := range dependencies {
				found = found || key == r.SubjectStepKey
			}
			if !found {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// DecodeDeclaration requires one unique result for every predicate and rejects unknown fields,
// trailing JSON and duplicate keys through canonical token scanning before semantic use.
// DecodeDeclaration требует по результату на каждое условие и отклоняет неизвестные поля,
// лишний JSON и повторные ключи до семантического использования.
func DecodeDeclaration(raw string, requirements []entity.WorkflowResultRequirement) (map[string]entity.WorkflowDeclaredResult, error) {
	if len(requirements) < 1 || len(requirements) > 4 || len(raw) > MaximumResultBytes || !utf8.ValidString(raw) || !uniqueJSON([]byte(raw)) {
		return nil, ErrResultInvalid
	}
	// Exact keys prevent encoding/json's case-insensitive field matching from widening the schema.
	// Точные ключи не позволяют нечувствительному к регистру encoding/json расширить схему.
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) != nil || !exactFields(fields, "schema", "results") {
		return nil, ErrResultInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var declaration declarationWire
	if decoder.Decode(&declaration) != nil || decoder.Decode(new(any)) != io.EOF || declaration.Schema != "kodex.workflow-result.v1" || len(declaration.Results) != len(requirements) {
		return nil, ErrResultInvalid
	}
	known := map[string]string{}
	for _, r := range requirements {
		if !resultKey.MatchString(r.Key) || known[r.Key] != "" {
			return nil, ErrResultInvalid
		}
		known[r.Key] = r.Kind
	}
	var rawResults []json.RawMessage
	if json.Unmarshal(fields["results"], &rawResults) != nil || len(rawResults) != len(requirements) {
		return nil, ErrResultInvalid
	}
	result := map[string]entity.WorkflowDeclaredResult{}
	for index, wire := range declaration.Results {
		r, valid := wire.declaredResult()
		if !valid {
			return nil, ErrResultInvalid
		}
		kind, ok := known[r.Key]
		if !ok {
			return nil, ErrResultInvalid
		}
		if _, duplicate := result[r.Key]; duplicate {
			return nil, ErrResultInvalid
		}
		var resultFields map[string]json.RawMessage
		if json.Unmarshal(rawResults[index], &resultFields) != nil {
			return nil, ErrResultInvalid
		}
		switch kind {
		case "ARTIFACT":
			if !exactFields(resultFields, "key", "source") || !validArtifactSource(resultFields["source"], r.Source) {
				return nil, ErrResultInvalid
			}
		case "PULL_REQUEST", "INTEGRATION_RECEIPT", "DEPLOYMENT":
			if !exactFields(resultFields, "key", "receiptRef") || !receiptRefPattern.MatchString(r.ReceiptRef) {
				return nil, ErrResultInvalid
			}
		case "REVIEW", "QA":
			if !exactFields(resultFields, "key", "headSha", "verdict") || !CommitPattern.MatchString(r.HeadSHA) || (r.Verdict != "PASS" && r.Verdict != "CHANGES_REQUIRED") {
				return nil, ErrResultInvalid
			}
		default:
			return nil, ErrResultInvalid
		}
		result[r.Key] = r
	}
	return result, nil
}

// exactFields requires each named member once with a non-null value and permits no other spelling.
// Duplicate detection belongs to uniqueJSON; callers still validate each member's concrete type.
// exactFields требует все названные ненулевые поля и запрещает другие написания. Повторы проверяет
// uniqueJSON; вызывающая сторона дополнительно проверяет конкретный тип каждого значения.
func exactFields(fields map[string]json.RawMessage, names ...string) bool {
	if len(fields) != len(names) {
		return false
	}
	for _, name := range names {
		raw, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return false
		}
	}
	return true
}

// validArtifactSource checks the closed selector union, never the referenced artifact's authority.
// The owner must resolve the current prepared capture or exact catalog and business-attempt lineage.
// validArtifactSource проверяет закрытое объединение селекторов, но не полномочия на артефакт.
// Владелец отдельно разрешает текущий capture либо точный каталог и происхождение бизнес-попытки.
func validArtifactSource(raw json.RawMessage, source *entity.WorkflowArtifactSource) bool {
	if source == nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	switch source.Kind {
	case "CURRENT_COMPLETION":
		return exactFields(fields, "kind")
	case "CAPTURED_ARTIFACT":
		return exactFields(fields, "kind", "artifactRef", "revision", "digest") &&
			artifactRefPattern.MatchString(source.ArtifactRef) && source.Revision > 0 &&
			source.Revision <= 9007199254740991 && digestPattern.MatchString(source.Digest)
	default:
		return false
	}
}

// uniqueJSON rejects duplicate object members recursively before decoding a result declaration.
// uniqueJSON отклоняет повторные поля объектов рекурсивно до декодирования результата.
func uniqueJSON(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	depth := 0
	var value func() bool
	value = func() bool {
		token, err := d.Token()
		if err != nil {
			return false
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return true
		}
		depth++
		if depth > 8 {
			return false
		}
		defer func() { depth-- }()
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value() {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !value() {
					return false
				}
			}
			end, err := d.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !value() {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

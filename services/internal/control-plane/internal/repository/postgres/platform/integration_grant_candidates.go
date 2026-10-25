package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryIntegrationGrantCandidates embeds staged candidate projection from authoritative admission
// with visibility and unavailable-shipped-OpenAPI filtering before count and pagination. It groups
// readiness, applies search/cursor and returns bounded JSON rows plus total; result metadata does
// not authorize a later grant.
// queryIntegrationGrantCandidates встраивает поэтапную проекцию кандидатов из авторитетного
// допуска с проверкой видимости и исключением непригодного поставленного OpenAPI до подсчёта и
// пагинации. Группирует готовность, применяет поиск и курсор, возвращает ограниченные JSON-строки
// и количество; метаданные не разрешают последующую выдачу grant.
//
//go:embed sql/integration_grant_candidates.sql
var queryIntegrationGrantCandidates string

// queryIntegrationCandidatePins embeds current connection/package, project and typed recipient
// version pins within one organization, using empty/zero values when unspecified or missing. It
// performs no actor permission check itself.
// queryIntegrationCandidatePins встраивает текущие версии подключения, пакета, проекта и
// типизированного получателя внутри организации, возвращая пустые либо нулевые значения при
// отсутствии. Сам запрос не проверяет права субъекта.
//
//go:embed sql/integration_candidate_pins.sql
var queryIntegrationCandidatePins string

// integrationCandidateRow decodes one staged candidate with exact
// connection/package/project/recipient pins, capability and readiness reason. It is a JSON read
// projection passed to decoration, not a grant or durable execution snapshot.
// integrationCandidateRow декодирует одного кандидата стадии с точными версиями подключения,
// пакета, проекта и получателя, capability и причиной готовности. Это JSON-представление чтения
// для дополнения, а не grant или устойчивый снимок исполнения.
type integrationCandidateRow struct {
	Ref               string `json:"ref"`
	Name              string `json:"name"`
	ConnectionRef     string `json:"connection_ref"`
	ConnectionVersion int64  `json:"connection_version"`
	DefinitionKey     string `json:"definition_key"`
	DefinitionVersion string `json:"definition_version"`
	DefinitionDigest  string `json:"definition_digest"`
	ProviderName      string `json:"provider_name"`
	ProjectRef        string `json:"project_ref"`
	ProjectVersion    int64  `json:"project_version"`
	RecipientKind     string `json:"recipient_kind"`
	RecipientRef      string `json:"recipient_ref"`
	RecipientVersion  int64  `json:"recipient_version"`
	CapabilityKey     string `json:"capability_key"`
	Reason            string `json:"reason"`
}

// integrationCandidateCursor carries the context digest and last candidate ref in an unsigned
// base64 JSON page token. The server recomputes the digest and current eligibility; possession of
// the cursor is not authority.
// integrationCandidateCursor хранит дайджест контекста и последнюю ссылку кандидата в
// неподписанном base64 JSON токене страницы. Сервер заново вычисляет дайджест и текущую
// допустимость; владение курсором не даёт полномочий.
type integrationCandidateCursor struct{ Scope, After string }

// validateIntegrationCandidates validates bounded UTF-8 search, bounded reference syntax,
// recipient kind and capability key, then enforces the closed GRANT stage context or
// CONNECTION/USE workflow-step pairing. It does no lookup or access check and leaves page
// token/size validation to the read path.
// validateIntegrationCandidates проверяет ограниченный UTF-8 поиск, форму и размер ссылок, вид
// получателя и ключ capability, затем закрытый контекст стадий GRANT либо пару workflow/step для
// CONNECTION/USE. Не обращается к хранилищу и не проверяет доступ; токен и размер страницы
// проверяются в пути чтения.
func validateIntegrationCandidates(stage string, input query.IntegrationCandidates) error {
	c := input.Context
	if !utf8.ValidString(input.Filter.Query) || len([]rune(input.Filter.Query)) > 200 || strings.ContainsRune(input.Filter.Query, '\x00') {
		return errs.ErrInvalid
	}
	for _, ref := range []string{c.ConnectionRef, c.ProjectRef, c.RecipientRef, c.WorkflowRef} {
		if len(ref) > 96 || strings.ContainsAny(ref, "\x00\r\n/ ") {
			return errs.ErrInvalid
		}
	}
	if c.RecipientKind != "" && c.RecipientKind != "AGENT" && c.RecipientKind != "WORKFLOW" {
		return errs.ErrInvalid
	}
	if c.CapabilityKey != "" && !validCapabilityKey(c.CapabilityKey) {
		return errs.ErrInvalid
	}
	if stage == "CONNECTION" && input.Purpose == "USE" {
		if c.ConnectionRef != "" || c.ProjectRef == "" || c.RecipientKind != "AGENT" || c.RecipientRef == "" || c.CapabilityKey == "" || (c.WorkflowRef == "") != (c.StepKey == "") || len(c.StepKey) > 96 {
			return errs.ErrInvalid
		}
		return nil
	}
	if input.Purpose != "GRANT" || c.WorkflowRef != "" || c.StepKey != "" || c.CapabilityKey != "" {
		return errs.ErrInvalid
	}
	switch stage {
	case "CONNECTION":
		if c.ConnectionRef != "" || c.ProjectRef != "" || c.RecipientKind != "" || c.RecipientRef != "" {
			return errs.ErrInvalid
		}
	case "PROJECT":
		if c.ConnectionRef == "" || c.ProjectRef != "" || c.RecipientKind != "" || c.RecipientRef != "" {
			return errs.ErrInvalid
		}
	case "RECIPIENT":
		if c.ConnectionRef == "" || c.ProjectRef == "" || c.RecipientKind == "" || c.RecipientRef != "" {
			return errs.ErrInvalid
		}
	case "CAPABILITY":
		if c.ConnectionRef == "" || c.ProjectRef == "" || c.RecipientKind == "" || c.RecipientRef == "" {
			return errs.ErrInvalid
		}
	default:
		return errs.ErrInvalid
	}
	return nil
}

// integrationCandidates validates input, opens a five-second read-only repeatable-read snapshot
// and authorizes selected context resources before reading current pins. It binds cursor scope to
// tenant/actor/authority project, stage, query and pins, reads one extra row, invokes the
// decorator and commits. Errors may leave partial context or decorator effects; the result must
// not be issued as a successful page or grant.
// integrationCandidates проверяет вход, открывает read-only RepeatableRead снимок с бюджетом пять
// секунд и проверяет доступ к выбранным ресурсам контекста до чтения текущих версий. Связывает
// курсор с организацией, субъектом, проектом полномочий, стадией, запросом и версиями, читает
// дополнительную строку, вызывает дополнение и завершает транзакцию. Ошибки могут оставлять
// частичный контекст или эффекты дополнения; результат нельзя выдавать как успешную страницу или
// grant.
func (repository *Repository) integrationCandidates(ctx context.Context, principal value.Principal, stage string, input query.IntegrationCandidates, decorate func(context.Context, pgx.Tx, scope, integrationCandidateRow, entity.IntegrationCandidatePins) error) (entity.IntegrationCandidatePage, error) {
	result := entity.IntegrationCandidatePage{}
	if err := validateIntegrationCandidates(stage, input); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	c := input.Context
	result.Context = entity.IntegrationCandidateContext(c)
	for _, selected := range []struct{ kind, ref, permission string }{
		{"INTEGRATION", c.ConnectionRef, "integration.view"},
		{"PROJECT", c.ProjectRef, "project.view"},
		{c.RecipientKind, c.RecipientRef, strings.ToLower(c.RecipientKind) + ".view"},
	} {
		if selected.ref == "" {
			continue
		}
		target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: selected.kind, ResourceRef: selected.ref})
		if err != nil {
			return result, err
		}
		if selected.kind != "INTEGRATION" && c.ProjectRef != "" && target.scope.ProjectRef != c.ProjectRef {
			return result, errs.ErrNotFound
		}
		if err := repository.requireAccess(ctx, tx, current, selected.permission, target); err != nil {
			return result, err
		}
	}
	err = tx.QueryRow(ctx, queryIntegrationCandidatePins, pgx.StrictNamedArgs{"organization_id": current.organizationID,
		"connection_ref": c.ConnectionRef, "project_ref": c.ProjectRef, "recipient_kind": c.RecipientKind, "recipient_ref": c.RecipientRef,
	}).Scan(&result.Pins.ConnectionVersion, &result.Pins.DefinitionVersion, &result.Pins.DefinitionDigest, &result.Pins.ProjectVersion, &result.Pins.RecipientVersion)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var required []string
	if c.WorkflowRef != "" {
		workflow := entity.AgentEffectiveCapabilities{AgentRef: c.RecipientRef, ProjectRef: c.ProjectRef, WorkflowRef: c.WorkflowRef, StepKey: c.StepKey}
		required, err = repository.effectiveWorkflowCapabilities(ctx, tx, current, &workflow)
		if err != nil {
			return result, err
		}
		result.Pins.WorkflowRevisionRef = workflow.WorkflowVersionRef
	}
	// Cursor связывает actor и все зависимые поля; повторное чтение всегда
	// вычисляет count и eligibility по текущему owner snapshot.
	scopeBytes, _ := json.Marshal(struct {
		Tenant, Actor, Project, Stage string
		Query                         query.IntegrationCandidates
		Pins                          entity.IntegrationCandidatePins
	}{current.organizationID, current.actorID, current.authorityProjectID, stage, query.IntegrationCandidates{Purpose: input.Purpose, Context: c, Filter: query.Filter{Query: input.Filter.Query}}, result.Pins})
	sum := sha256.Sum256(scopeBytes)
	result.ContextDigest = hex.EncodeToString(sum[:])
	result.Pins.ContextDigest = result.ContextDigest
	after := ""
	if input.Filter.Page.Token != "" {
		if len(input.Filter.Page.Token) > 2048 {
			return result, errs.ErrInvalid
		}
		raw, err := base64.RawURLEncoding.DecodeString(input.Filter.Page.Token)
		var cursor integrationCandidateCursor
		if err != nil || len(raw) > 1024 || decodeStrict(raw, &cursor) != nil || cursor.Scope != result.ContextDigest || cursor.After == "" {
			return result, errs.ErrInvalid
		}
		after = cursor.After
	}
	var raw []byte
	limit := boundedPage(input.Filter.Page)
	err = tx.QueryRow(ctx, queryIntegrationGrantCandidates, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project_id": current.authorityProjectID,
		"connection_ref": c.ConnectionRef, "project_ref": c.ProjectRef, "recipient_kind": c.RecipientKind, "recipient_ref": c.RecipientRef,
		"capability_key": c.CapabilityKey, "purpose": input.Purpose, "workflow_capabilities": required,
		"stage": stage, "query": input.Filter.Query, "after_ref": after, "page_limit": limit + 1,
	}).Scan(&result.Total, &raw)
	if err != nil || result.Total < 0 || result.Total > 1<<53-1 {
		return result, errs.ErrUnavailable
	}
	var rows []integrationCandidateRow
	if json.Unmarshal(raw, &rows) != nil || len(rows) > int(limit)+1 {
		return result, errs.ErrUnavailable
	}
	if len(rows) > int(limit) {
		rows = rows[:limit]
		cursor, _ := json.Marshal(integrationCandidateCursor{Scope: result.ContextDigest, After: rows[len(rows)-1].Ref})
		result.NextPageToken = base64.RawURLEncoding.EncodeToString(cursor)
	}
	for _, row := range rows {
		pins := entity.IntegrationCandidatePins{ContextDigest: result.ContextDigest, ConnectionVersion: row.ConnectionVersion,
			DefinitionVersion: row.DefinitionVersion, DefinitionDigest: row.DefinitionDigest, ProjectVersion: row.ProjectVersion,
			RecipientVersion: row.RecipientVersion, WorkflowRevisionRef: result.Pins.WorkflowRevisionRef}
		if err := decorate(ctx, tx, current, row, pins); err != nil {
			return result, err
		}
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

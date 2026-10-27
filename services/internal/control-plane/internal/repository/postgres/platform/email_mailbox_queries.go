package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryEmailMailboxLatestRevision Reads the highest revision ref for the exact
// organization/configuration set without imposing a publication state. Caller owner resolution and
// view decoding determine eligibility.
// queryEmailMailboxLatestRevision Читает ссылку наибольшей ревизии точного набора организации без
// фильтра публикации. Пригодность определяют разрешение владельца и декодирование представления.
//
//go:embed sql/email_mailbox_configuration_latest_revision.sql
var queryEmailMailboxLatestRevision string

// queryEmailMailboxBinding Reads the configuration/revision bound to an exact
// organization/connection through matching owner joins. Absence is allowed by callers while
// selecting another owned configuration.
// queryEmailMailboxBinding Читает конфигурацию и ревизию привязки точного соединения организации
// через совпавшие связи владельца. Отсутствие допускается при выборе другой принадлежащей
// конфигурации.
//
//go:embed sql/email_mailbox_configuration_binding.sql
var queryEmailMailboxBinding string

// queryEmailMailboxList Lists exact organization/connection-owned nondeleted email configuration
// refs with search, ascending cursor and filtered total, retaining a totals-only row for an empty
// page. Actor permission is checked before this query.
// queryEmailMailboxList Выдаёт ссылки конфигураций точного неудалённого email-соединения
// организации с поиском, возрастающим курсором и total после фильтра, сохраняя строку итогов при
// пустой странице. Право субъекта проверяется до запроса.
//
//go:embed sql/email_mailbox_configuration_list.sql
var queryEmailMailboxList string

// queryEmailMailboxCredentialReceipt Reads a nonexpired idempotency response only for exact
// organization/actor/operation/key. The caller additionally checks the decoded receipt connection
// before returning credential metadata.
// queryEmailMailboxCredentialReceipt Читает неистёкший идемпотентный ответ по точным организации,
// субъекту, операции и ключу. Вызывающий дополнительно сверяет соединение декодированной квитанции
// до выдачи метаданных credential.
//
//go:embed sql/email_mailbox_credential_receipt.sql
var queryEmailMailboxCredentialReceipt string

// queryEmailMailboxCredentialsList Pages descriptor name/generation/kind for exact
// organization/connection and optional kind, using a fixed-width generation cursor and filtered
// total. It returns no credential values; connection authority is checked outside SQL.
// queryEmailMailboxCredentialsList Выдаёт страницу name, generation и kind дескрипторов точного
// соединения организации с необязательным видом, курсором фиксированной ширины поколения и total.
// Значений credential нет; полномочия на соединение проверяются вне SQL.
//
//go:embed sql/email_mailbox_credentials_list.sql
var queryEmailMailboxCredentialsList string

// queryEmailMailboxActionState Reads exact connection enablement, mutable drafts for the selected
// set and global pending-publication existence to derive UI actions. These hints neither mutate
// configuration nor authorize an action.
// queryEmailMailboxActionState Читает включённость точного соединения, изменяемые черновики
// выбранного набора и наличие общей pending-публикации для UI-действий. Подсказки не меняют
// конфигурацию и не разрешают команду.
//
//go:embed sql/email_mailbox_action_state.sql
var queryEmailMailboxActionState string

// PreviewEmailMailboxConfiguration Authorizes and locks the exact connection,
// decodes/canonicalizes a proposed specification and materializes a temporary mailbox to validate
// credential references even if delivery is disabled. Syntax/configuration/credential issues
// become safe diagnostics; preview rolls back and neither publishes nor grants authority.
// PreviewEmailMailboxConfiguration Авторизует и блокирует точное соединение, разбирает и
// канонизирует предложение, материализует временный mailbox для проверки credential-ссылок даже
// при выключенной доставке. Ошибки формы, конфигурации и credential дают безопасную диагностику;
// preview откатывается, ничего не публикует и не выдаёт полномочия.
func (repository *Repository) PreviewEmailMailboxConfiguration(ctx context.Context, principal value.Principal, connectionRef, format, content string) (entity.EmailMailboxPreview, error) {
	current, tx, version, err := repository.beginEmailMailboxRead(ctx, principal, connectionRef)
	if err != nil {
		return entity.EmailMailboxPreview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	specification, err := emailpolicy.DecodeSpecification(format, content)
	if err != nil {
		return entity.EmailMailboxPreview{Diagnostics: []entity.EmailMailboxDiagnostic{emailpolicy.Diagnostic(emailpolicy.DiagnosticSyntax)}}, nil
	}
	canonical, err := emailpolicy.CanonicalYAML(specification)
	if err != nil {
		return entity.EmailMailboxPreview{}, errs.ErrUnavailable
	}
	result := entity.EmailMailboxPreview{Specification: &specification, CanonicalYAML: canonical}
	mailboxRef, err := newMailboxRef()
	if err != nil {
		return entity.EmailMailboxPreview{}, errs.ErrUnavailable
	}
	mailbox, err := emailpolicy.MaterializeMailbox(specification, emailpolicy.MailboxBinding{Ref: mailboxRef,
		OrganizationRef: current.organizationRef, ConnectionRef: connectionRef, Revision: 1, CredentialGeneration: version})
	if err != nil {
		result.Diagnostics = []entity.EmailMailboxDiagnostic{emailpolicy.Diagnostic(emailpolicy.DiagnosticConfiguration)}
		return result, nil
	}
	// Preview проверяет ссылки независимо от включения доставки.
	mailbox.Enabled = true
	if _, err := emailCredentialDigests(ctx, tx, api.Configuration{Mailboxes: []api.Mailbox{mailbox}}); err != nil {
		if errors.Is(err, errs.ErrConflict) {
			result.Diagnostics = []entity.EmailMailboxDiagnostic{emailpolicy.Diagnostic(emailpolicy.DiagnosticCredential)}
			return result, nil
		}
		return entity.EmailMailboxPreview{}, err
	}
	result.Valid = true
	return result, nil
}

// ListEmailMailboxCredentials Under authorized connection lock, validates a scope-bound catalog
// cursor and reads limit+1 descriptor rows, preserving nil empty results, filtered total and next
// name/generation token. Scan/iteration failures discard partial results; the read transaction
// rolls back on exit without commit.
// ListEmailMailboxCredentials Под блокировкой разрешённого соединения проверяет привязанный к
// области курсор, читает limit+1 дескрипторов, сохраняет nil пустого результата, total и следующий
// токен имени с поколением. Сбой чтения отбрасывает частичный результат; транзакция откатывается
// без commit.
func (repository *Repository) ListEmailMailboxCredentials(ctx context.Context, principal value.Principal, connectionRef, kind string, page query.Page) ([]entity.EmailMailboxCredential, int64, string, error) {
	current, tx, _, err := repository.beginEmailMailboxRead(ctx, principal, connectionRef)
	if err != nil {
		return nil, 0, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	filter := query.Filter{ResourceRef: connectionRef, Category: kind, Page: page}
	cursor, err := decodeCatalogCursor(current, "EMAIL_CREDENTIAL", filter)
	if err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(page)
	rows, err := tx.Query(ctx, queryEmailMailboxCredentialsList, current.organizationID, connectionRef, kind, cursor, limit+1)
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var total int64
	var result []entity.EmailMailboxCredential
	for rows.Next() {
		var item entity.EmailMailboxCredential
		if rows.Scan(&item.Name, &item.Generation, &item.Kind, &total) != nil {
			return nil, 0, "", errs.ErrUnavailable
		}
		if item.Name != "" {
			item.ConnectionRef, item.ConnectionVersion = connectionRef, item.Generation
			result = append(result, item)
		}
	}
	if rows.Err() != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	next := ""
	if len(result) > int(limit) {
		result = result[:limit]
		last := result[len(result)-1]
		next = encodeCatalogCursor(current, "EMAIL_CREDENTIAL", filter, fmt.Sprintf("%s.%020d", last.Name, last.Generation))
	}
	return result, total, next, nil
}

// requireEmailMailboxConnection Requires integration.manage, hiding access failure as not-found,
// then locks an exact same-organization nondeleted email connection and returns its version.
// Missing rows are not-found, other read errors unavailable; no external mailbox is accessed.
// requireEmailMailboxConnection Требует integration.manage, скрывая отказ как NotFound, блокирует
// точное неудалённое email-соединение той же организации и возвращает версию. Нет строки —
// NotFound, иной сбой — Unavailable; внешний ящик не читается.
func (repository *Repository) requireEmailMailboxConnection(ctx context.Context, tx pgx.Tx, current scope, ref string) (int64, error) {
	if err := repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: ref}); err != nil {
		return 0, errs.ErrNotFound
	}
	var id string
	var version int64
	if err := tx.QueryRow(ctx, queryEmailCredentialLock, current.organizationID, ref).Scan(&id, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, errs.ErrNotFound
		}
		return 0, errs.ErrUnavailable
	}
	return version, nil
}

// emailMailboxViewTx Resolves the selected/bound owned mailbox set and revision, decodes
// specification and closed diagnostics, reads publication/action state and projects next actions.
// It rejects foreign owner bindings and unknown diagnostics; hints do not replace subsequent
// mutation authority.
// emailMailboxViewTx Разрешает выбранные либо привязанные принадлежащие набор и ревизию mailbox,
// декодирует спецификацию и закрытую диагностику, читает публикацию и действия. Отклоняет чужую
// привязку и неизвестную диагностику; подсказки не заменяют полномочия изменения.
func (repository *Repository) emailMailboxViewTx(ctx context.Context, tx pgx.Tx, current scope, connectionRef string, connectionVersion int64, configurationRef, revisionRef string) (entity.EmailMailboxConfigurationView, error) {
	var boundConfiguration, boundRevision string
	err := tx.QueryRow(ctx, queryEmailMailboxBinding, current.organizationID, connectionRef).Scan(&boundConfiguration, &boundRevision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return entity.EmailMailboxConfigurationView{}, errs.ErrUnavailable
	}
	if configurationRef == "" {
		configurationRef = boundConfiguration
	}
	var ownerConnection, mailboxRef string
	if err := tx.QueryRow(ctx, queryEmailMailboxConfigurationOwner, current.organizationID, configurationRef).Scan(&ownerConnection, &mailboxRef); err != nil || ownerConnection != connectionRef {
		return entity.EmailMailboxConfigurationView{}, errs.ErrNotFound
	}
	set, err := repository.resolveManagedSet(ctx, tx, current, command.ManagedConfigurationInput{ConfigurationRef: configurationRef}, "EMAIL_MAILBOX", false)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, err
	}
	if revisionRef == "" {
		if err := tx.QueryRow(ctx, queryEmailMailboxLatestRevision, current.organizationID, set.id).Scan(&revisionRef); err != nil {
			return entity.EmailMailboxConfigurationView{}, errs.ErrUnavailable
		}
	}
	revision, err := repository.lockManagedRevision(ctx, tx, current, set, revisionRef)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, err
	}
	specification, err := emailpolicy.DecodeSpecification(revision.ContentFormat, revision.Content)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, errs.ErrUnavailable
	}
	view := entity.EmailMailboxConfigurationView{ConnectionRef: connectionRef, ConnectionVersion: connectionVersion,
		MailboxRef: mailboxRef, Configuration: set.ManagedConfigurationSet, Revision: revision.ManagedConfigurationRevision, Specification: specification}
	if boundConfiguration == configurationRef {
		view.BoundRevisionRef = boundRevision
	}
	for _, text := range revision.ValidationDiagnostics {
		code, _, _ := strings.Cut(text, ":")
		switch code {
		case emailpolicy.DiagnosticSyntax, emailpolicy.DiagnosticConfiguration, emailpolicy.DiagnosticCredential:
			view.Diagnostics = append(view.Diagnostics, emailpolicy.Diagnostic(code))
		default:
			return entity.EmailMailboxConfigurationView{}, errs.ErrUnavailable
		}
	}
	view.Publication, err = emailMailboxPublicationView(ctx, tx, current, connectionRef)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, err
	}
	actionState := emailpolicy.ActionState{ManagedBy: set.ManagedBy, RevisionState: revision.State, HasPublishedRevision: set.CurrentRevision != nil, Bound: view.BoundRevisionRef != ""}
	if err := tx.QueryRow(ctx, queryEmailMailboxActionState, current.organizationID, connectionRef, set.Ref).Scan(&actionState.ConnectionEnabled, &actionState.HasMutableDraft, &actionState.PendingDelivery); err != nil {
		return entity.EmailMailboxConfigurationView{}, errs.ErrUnavailable
	}
	view.NextActions = emailpolicy.AvailableActions(actionState)
	return view, nil
}

// GetEmailMailboxConfiguration Resolves current scope, opens the existing ordinary read
// transaction and requires the exact managed connection before building its owned revision view.
// The caller-facing read returns after view evaluation and rolls back without commit, retaining
// existing error mappings.
// GetEmailMailboxConfiguration Разрешает текущую область, открывает прежнюю обычную транзакцию
// чтения и требует точное разрешённое соединение до построения представления ревизии. Возвращает
// результат после вычисления view и откатывается без commit, сохраняя преобразования ошибок.
func (repository *Repository) GetEmailMailboxConfiguration(ctx context.Context, principal value.Principal, connectionRef, configurationRef, revisionRef string) (entity.EmailMailboxConfigurationView, error) {
	current, tx, version, err := repository.beginEmailMailboxRead(ctx, principal, connectionRef)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return repository.emailMailboxViewTx(ctx, tx, current, connectionRef, version, configurationRef, revisionRef)
}

// ListEmailMailboxConfigurations Authorizes/locks the exact connection, pages owned set refs and
// closes rows before hydrating each complete view in the same transaction. Preserves total/cursor
// and page-level CREATE_DRAFT hint; any failed view aborts the whole result. No read commit
// occurs.
// ListEmailMailboxConfigurations Авторизует и блокирует точное соединение, читает страницу
// принадлежащих наборов и закрывает rows до полного разрешения каждого view в той же транзакции.
// Сохраняет total, курсор и общую CREATE_DRAFT подсказку; сбой любого view отклоняет весь
// результат. Commit чтения не выполняется.
func (repository *Repository) ListEmailMailboxConfigurations(ctx context.Context, principal value.Principal, connectionRef, search string, page query.Page) (entity.EmailMailboxPage, error) {
	current, tx, version, err := repository.beginEmailMailboxRead(ctx, principal, connectionRef)
	if err != nil {
		return entity.EmailMailboxPage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	filter := query.Filter{ResourceRef: connectionRef, Query: search, Page: page}
	cursor, err := decodeCatalogCursor(current, "EMAIL_MAILBOX", filter)
	if err != nil {
		return entity.EmailMailboxPage{}, err
	}
	limit := boundedPage(page)
	rows, err := tx.Query(ctx, queryEmailMailboxList, current.organizationID, connectionRef, search, cursor, limit+1)
	if err != nil {
		return entity.EmailMailboxPage{}, errs.ErrUnavailable
	}
	result := entity.EmailMailboxPage{NextActions: []entity.EmailMailboxActionAvailability{{Action: "CREATE_DRAFT", Enabled: true, Reason: "NONE"}}}
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref, &result.Total); err != nil {
			rows.Close()
			return entity.EmailMailboxPage{}, errs.ErrUnavailable
		}
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return entity.EmailMailboxPage{}, errs.ErrUnavailable
	}
	if len(refs) > int(limit) {
		refs = refs[:limit]
		result.NextPageToken = encodeCatalogCursor(current, "EMAIL_MAILBOX", filter, refs[len(refs)-1])
	}
	for _, ref := range refs {
		view, err := repository.emailMailboxViewTx(ctx, tx, current, connectionRef, version, ref, "")
		if err != nil {
			return entity.EmailMailboxPage{}, err
		}
		result.Items = append(result.Items, view)
	}
	return result, nil
}

// GetEmailMailboxCredentialReceipt Requires current connection authority, reads exact
// actor/operation/key receipt, decodes a nonnil EmailCredential and rejects a different
// connection. Returns name/generation/kind/version only, never a value; the read transaction rolls
// back.
// GetEmailMailboxCredentialReceipt Требует текущее право на соединение, читает квитанцию точных
// субъекта, операции и ключа, декодирует непустую EmailCredential и отклоняет другое соединение.
// Возвращает только name, generation, kind и version без значения; транзакция откатывается.
func (repository *Repository) GetEmailMailboxCredentialReceipt(ctx context.Context, principal value.Principal, connectionRef, key string) (entity.EmailMailboxCredential, error) {
	current, tx, _, err := repository.beginEmailMailboxRead(ctx, principal, connectionRef)
	if err != nil {
		return entity.EmailMailboxCredential{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var raw []byte
	if err := tx.QueryRow(ctx, queryEmailMailboxCredentialReceipt, current.organizationID, current.actorID, key, "controlplane."+strings.ToLower(string(command.ConfigureEmailCredential))).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.EmailMailboxCredential{}, errs.ErrNotFound
		}
		return entity.EmailMailboxCredential{}, errs.ErrUnavailable
	}
	var receipt command.Result
	if json.Unmarshal(raw, &receipt) != nil || receipt.EmailCredential == nil {
		return entity.EmailMailboxCredential{}, errs.ErrUnavailable
	}
	credential := receipt.EmailCredential
	if credential.ConnectionRef != connectionRef {
		return entity.EmailMailboxCredential{}, errs.ErrNotFound
	}
	return entity.EmailMailboxCredential{Name: credential.Name, Generation: credential.Generation, Kind: credential.Kind,
		ConnectionRef: connectionRef, ConnectionVersion: credential.ConnectionVersion}, nil
}

// beginEmailMailboxRead resolves the caller scope, starts the existing ordinary transaction and
// requires integration.manage plus an exact nondeleted email-connection row lock. It preserves
// plain Begin options and error classes, rolling back with the request context on failed access;
// rollback errors remain ignored. Successful callers retain the same transaction/version and own
// its deferred rollback without commit. It does not apply configuration or grant new authority.
// beginEmailMailboxRead разрешает область клиента, открывает прежнюю обычную транзакцию и требует
// integration.manage с блокировкой точного неудалённого email-соединения. Сохраняет параметры
// обычного Begin и классы ошибок; при отказе откатывает с контекстом запроса, игнорируя ошибку
// rollback. При успехе вызывающий получает ту же транзакцию и версию, сам откатывает её без commit.
// Конфигурацию не применяет и новых полномочий не выдаёт.
func (repository *Repository) beginEmailMailboxRead(ctx context.Context, principal value.Principal, connectionRef string) (scope, pgx.Tx, int64, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return scope{}, nil, 0, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return scope{}, nil, 0, errs.ErrUnavailable
	}
	version, err := repository.requireEmailMailboxConnection(ctx, tx, current, connectionRef)
	if err != nil {
		_ = tx.Rollback(ctx)
		return scope{}, nil, 0, err
	}
	return current, tx, version, nil
}

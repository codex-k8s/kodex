package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryConfigOverlayHistoryList embeds the bounded descending revision/ref history query for one
// organization/agent, limited to published or superseded rows with publication time. Text and
// before-position predicates are SQL parameters; actor eligibility is checked by the surrounding
// transaction.
// queryConfigOverlayHistoryList встраивает запрос ограниченной истории одного organization/agent в
// порядке убывания revision/ref только для опубликованных или замещённых строк со временем
// публикации. Текст и позиция before передаются SQL-параметрами; доступ субъекта проверяется
// окружающей транзакцией.
//
//go:embed sql/config_overlay_history_list.sql
var queryConfigOverlayHistoryList string

// queryConfigOverlayHistoryCount embeds the count under the same organization/agent,
// published-history and text predicates as the list, without its page-position predicate. The
// caller bounds the count to the exact JavaScript integer range.
// queryConfigOverlayHistoryCount встраивает count с теми же organization/agent, published-history
// и текстовыми условиями, что список, без условия позиции страницы. Вызывающий ограничивает count
// точным целочисленным диапазоном JavaScript.
//
//go:embed sql/config_overlay_history_count.sql
var queryConfigOverlayHistoryCount string

// queryConfigOverlayHistoryGet embeds exact organization/agent/revision lookup of published or
// superseded overlay content and diagnostics. It supplies both direct reads and cursor-position
// resolution; it does not itself establish actor permission.
// queryConfigOverlayHistoryGet встраивает поиск точных organization/agent/revision для
// опубликованного либо замещённого overlay content и diagnostics. Обслуживает адресное чтение и
// разрешение позиции cursor; сам не устанавливает право субъекта.
//
//go:embed sql/config_overlay_history_get.sql
var queryConfigOverlayHistoryGet string

// beginScopedSnapshotRead resolves the principal before opening a ReadOnly RepeatableRead
// transaction, preserving scope errors and mapping BeginTx failure to Unavailable. Success
// transfers the transaction to the caller, which must install rollback and perform resource
// authorization, reads and commit. It neither requires access.view nor retries, commits, spawns
// work or grants mutation authority; the caller context and deadline remain unchanged.
// beginScopedSnapshotRead разрешает principal до открытия ReadOnly RepeatableRead транзакции,
// сохраняя scope errors и отображая отказ BeginTx в Unavailable. При успехе передаёт транзакцию
// вызывающему, который обязан установить rollback и выполнить resource authorization, чтение и
// commit. Не требует access.view, не повторяет, не фиксирует, не запускает работу и не выдаёт
// mutation authority; context и deadline вызывающего сохраняются.
func (repository *Repository) beginScopedSnapshotRead(ctx context.Context, principal value.Principal) (scope, pgx.Tx, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return scope{}, nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return scope{}, nil, errs.ErrUnavailable
	}
	return current, tx, nil
}

// validOverlayHistoryRef accepts a nonempty ref of at most 96 bytes containing only ASCII letters,
// digits, underscore or hyphen. It checks locator syntax only and does not resolve identity,
// ownership or a revision prefix.
// validOverlayHistoryRef принимает непустой ref длиной до 96 байтов только из ASCII-букв, цифр,
// подчёркивания или дефиса. Проверяет лишь синтаксис locator, не разрешая identity, ownership или
// префикс revision.
func validOverlayHistoryRef(ref string) bool {
	if ref == "" || len(ref) > 96 {
		return false
	}
	for _, r := range ref {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// overlayHistoryScope resolves the agent runtime-configuration target for agent.view, rejects an
// authority-project mismatch as Forbidden and delegates exact resource access in the supplied
// transaction. Resolution and access errors retain their existing mapping; the helper does not
// open or finish a transaction.
// overlayHistoryScope разрешает agent runtime-configuration target для agent.view, отклоняет
// несовпадение authority-project как Forbidden и делегирует проверку точного resource access в
// переданной транзакции. Ошибки разрешения и доступа сохраняют прежнее отображение; helper не
// открывает и не завершает транзакцию.
func (repository *Repository) overlayHistoryScope(ctx context.Context, tx pgx.Tx, current scope, agentRef string) error {
	permission, target, err := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.view", agentRef)
	if err != nil {
		return err
	}
	if current.authorityProjectID != "" && current.authorityProjectID != target.projectID {
		return errs.ErrForbidden
	}
	return repository.requireAccess(ctx, tx, current, permission, target)
}

// ListConfigOverlayRevisions validates the agent ref and bounded UTF-8 search, then reads
// authorized history/count in one five-second ReadOnly RepeatableRead snapshot. It binds the
// cursor to scope/filter, resolves its exact revision position, validates each stored overlay and
// returns at most 20 rows plus a next cursor. Unknown cursor is Invalid, backend/commit failure is
// Unavailable; rollback remains deferred and no partial page is successful.
// ListConfigOverlayRevisions проверяет agent ref и ограниченный UTF-8 search, затем читает
// разрешённые history/count в одном ReadOnly RepeatableRead snapshot с бюджетом пять секунд.
// Связывает cursor со scope/filter, разрешает точную revision position, проверяет каждый
// сохранённый overlay и возвращает до 20 строк с next cursor. Неизвестный cursor даёт Invalid,
// backend/commit failure — Unavailable; rollback остаётся отложенным, частичная страница не
// становится успешной.
func (repository *Repository) ListConfigOverlayRevisions(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.ConfigOverlayVersion, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	filter.Query = strings.TrimSpace(filter.Query)
	if !validOverlayHistoryRef(filter.ResourceRef) || !utf8.ValidString(filter.Query) || strings.ContainsRune(filter.Query, '\x00') || len([]rune(filter.Query)) > 200 {
		return nil, 0, "", errs.ErrInvalid
	}
	current, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.overlayHistoryScope(ctx, tx, current, filter.ResourceRef); err != nil {
		return nil, 0, "", err
	}
	cursor, err := decodeCatalogCursor(current, "CONFIG_OVERLAY_HISTORY", filter)
	if err != nil {
		return nil, 0, "", err
	}
	var before int64
	if cursor != "" {
		item, err := scanConfigOverlayHistory(tx.QueryRow(ctx, queryConfigOverlayHistoryGet, current.organizationID, filter.ResourceRef, cursor))
		if errors.Is(err, errs.ErrNotFound) {
			return nil, 0, "", errs.ErrInvalid
		}
		if err != nil {
			return nil, 0, "", err
		}
		before = item.Revision
	}
	var total int64
	if err := tx.QueryRow(ctx, queryConfigOverlayHistoryCount, current.organizationID, filter.ResourceRef, filter.Query).Scan(&total); err != nil || total < 0 || total > 1<<53-1 {
		return nil, 0, "", errs.ErrUnavailable
	}
	limit := boundedPage(filter.Page)
	if limit > 20 {
		limit = 20
	}
	rows, err := tx.Query(ctx, queryConfigOverlayHistoryList, current.organizationID, filter.ResourceRef, filter.Query, before, cursor, limit+1)
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	items := make([]entity.ConfigOverlayVersion, 0, limit+1)
	for rows.Next() {
		item, err := scanConfigOverlayHistory(rows)
		if err != nil {
			rows.Close()
			return nil, 0, "", err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = encodeCatalogCursor(current, "CONFIG_OVERLAY_HISTORY", filter, items[len(items)-1].Ref)
	}
	if tx.Commit(ctx) != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	return items, total, next, nil
}

// GetConfigOverlayRevision requires syntactically bounded agent/revision refs and the cov_
// revision prefix, resolves current scope and agent.view, then scans one published history row in
// a five-second ReadOnly RepeatableRead transaction. Success requires commit; errors return an
// empty overlay and preserve the existing resource/backend distinctions. It does not activate or
// restore the historical configuration.
// GetConfigOverlayRevision требует синтаксически ограниченные agent/revision refs и префикс
// revision cov_, разрешает текущий scope и agent.view, затем читает одну опубликованную history
// row в ReadOnly RepeatableRead транзакции с бюджетом пять секунд. Успех требует commit; ошибки
// возвращают пустой overlay с прежним различением resource/backend failures. Историческую
// конфигурацию не активирует и не восстанавливает.
func (repository *Repository) GetConfigOverlayRevision(ctx context.Context, principal value.Principal, agentRef, revisionRef string) (entity.ConfigOverlayVersion, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !validOverlayHistoryRef(agentRef) || !validOverlayHistoryRef(revisionRef) || !strings.HasPrefix(revisionRef, "cov_") {
		return entity.ConfigOverlayVersion{}, errs.ErrInvalid
	}
	current, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.ConfigOverlayVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.overlayHistoryScope(ctx, tx, current, agentRef); err != nil {
		return entity.ConfigOverlayVersion{}, err
	}
	item, err := scanConfigOverlayHistory(tx.QueryRow(ctx, queryConfigOverlayHistoryGet, current.organizationID, agentRef, revisionRef))
	if err != nil {
		return entity.ConfigOverlayVersion{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.ConfigOverlayVersion{}, errs.ErrUnavailable
	}
	return item, nil
}

// scanConfigOverlayHistory scans the stored overlay and metadata, maps no rows to NotFound and
// other scan failures to Unavailable, sets Version from Revision, then validates content parsing,
// exact SHA-256, safe positive revision, size, published state/time and strict diagnostic
// decoding. Corrupt content returns an empty value with Unavailable; early scan errors may retain
// partially scanned fields alongside the error, which callers must discard.
// scanConfigOverlayHistory читает сохранённый overlay и metadata, отображает отсутствие строки в
// NotFound, прочий отказ scan — в Unavailable, назначает Version из Revision, затем проверяет
// parse content, точный SHA-256, безопасную положительную revision, размер, published state/time и
// строгий decode diagnostics. Повреждённый content возвращает пустое значение с Unavailable;
// ранние scan errors могут содержать частично прочитанные поля вместе с ошибкой, которые
// вызывающий обязан отбросить.
func scanConfigOverlayHistory(row rowScanner) (entity.ConfigOverlayVersion, error) {
	var item entity.ConfigOverlayVersion
	var messages, diagnostics []byte
	err := row.Scan(&item.Ref, &item.Revision, &item.State, &item.Content, &item.Digest, &messages, &item.CreatedAt, &item.PublishedAt, &diagnostics, &item.SchemaRevision, &item.SchemaDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, errs.ErrNotFound
	}
	if err != nil {
		return item, errs.ErrUnavailable
	}
	item.Version = item.Revision
	digest := sha256.Sum256([]byte(item.Content))
	if _, err := runtimecontract.ParseConfigOverlay(item.Content); err != nil || item.Digest != hex.EncodeToString(digest[:]) {
		return entity.ConfigOverlayVersion{}, errs.ErrUnavailable
	}
	if item.Revision <= 0 || item.Revision > 1<<53-1 || len(item.Content) > 65536 || item.PublishedAt == nil ||
		(item.State != "PUBLISHED" && item.State != "SUPERSEDED") || decodeStrict(messages, &item.ValidationMessages) != nil || decodeStrict(diagnostics, &item.Diagnostics) != nil {
		return entity.ConfigOverlayVersion{}, errs.ErrUnavailable
	}
	return item, nil
}

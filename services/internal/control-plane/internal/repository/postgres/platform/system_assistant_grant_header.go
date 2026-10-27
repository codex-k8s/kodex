package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// querySystemAssistantGrantHeader embeds the exact active organization connection header only
// outside an authority-project scope and with both organization.manage and integration.manage
// visibility. It exposes package/version pins without credential content or adding locks.
// querySystemAssistantGrantHeader встраивает заголовок точного активного подключения организации
// только вне области проекта полномочий и при видимости organization.manage и integration.manage.
// Раскрывает версии пакета и подключения без содержимого credentials и дополнительных блокировок.
//
//go:embed sql/system_assistant_grant__header.sql
var querySystemAssistantGrantHeader string

// systemAssistantGrantHeader reads the exact authorized SYSTEM connection header in the supplied
// transaction and checks identity/version/package metadata. Missing rows are NotFound, query or
// malformed-header failures Unavailable; it adds no lock, mutation or commit.
// systemAssistantGrantHeader читает точный разрешённый заголовок SYSTEM подключения в переданной
// транзакции и проверяет идентичность, версию и метаданные пакета. Отсутствие строки даёт
// NotFound, ошибка запроса или повреждённый заголовок — Unavailable; не добавляет блокировок,
// изменений и Commit.
func (repository *Repository) systemAssistantGrantHeader(ctx context.Context, tx pgx.Tx, current scope, connectionRef string) (assistantGrantConnectionHeader, error) {
	var header assistantGrantConnectionHeader
	err := tx.QueryRow(ctx, querySystemAssistantGrantHeader, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID, "connection_ref": connectionRef}).Scan(&header.ref, &header.name, &header.version, &header.definitionKey, &header.definitionVersion, &header.definitionDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return header, errs.ErrNotFound
	}
	if err != nil || !validAssistantGrantConnectionHeader(header, connectionRef) {
		return header, errs.ErrUnavailable
	}
	return header, nil
}

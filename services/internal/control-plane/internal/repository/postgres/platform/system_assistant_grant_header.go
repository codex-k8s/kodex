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

// systemAssistantGrantConnectionHeader holds private connection identity/name/version and exact
// package key/version/digest for the already admitted SYSTEM grant path. It is not a UI connection
// projection or persistent authority.
// systemAssistantGrantConnectionHeader хранит внутренние идентичность, имя и версию подключения и
// точные ключ, версию и дайджест пакета для уже допущенного SYSTEM grant пути. Это не
// UI-представление подключения и не устойчивые полномочия.
type systemAssistantGrantConnectionHeader struct {
	ref, name, definitionKey, definitionVersion, definitionDigest string
	version                                                       int64
}

// systemAssistantGrantHeader reads the exact authorized SYSTEM connection header in the supplied
// transaction and checks identity/version/package metadata. Missing rows are NotFound, query or
// malformed-header failures Unavailable; it adds no lock, mutation or commit.
// systemAssistantGrantHeader читает точный разрешённый заголовок SYSTEM подключения в переданной
// транзакции и проверяет идентичность, версию и метаданные пакета. Отсутствие строки даёт
// NotFound, ошибка запроса или повреждённый заголовок — Unavailable; не добавляет блокировок,
// изменений и Commit.
func (repository *Repository) systemAssistantGrantHeader(ctx context.Context, tx pgx.Tx, current scope, connectionRef string) (systemAssistantGrantConnectionHeader, error) {
	var header systemAssistantGrantConnectionHeader
	err := tx.QueryRow(ctx, querySystemAssistantGrantHeader, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID, "connection_ref": connectionRef}).Scan(&header.ref, &header.name, &header.version, &header.definitionKey, &header.definitionVersion, &header.definitionDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return header, errs.ErrNotFound
	}
	if err != nil || !validSystemAssistantGrantConnectionHeader(header, connectionRef) {
		return header, errs.ErrUnavailable
	}
	return header, nil
}

// validSystemAssistantGrantConnectionHeader requires an exact nonempty connection ref/name,
// positive connection version, nonempty package key/version and canonical lowercase digest syntax.
// It does not resolve the package or recheck actor permissions.
// validSystemAssistantGrantConnectionHeader требует точную непустую ссылку и имя подключения,
// положительную версию, непустые ключ и версию пакета и каноническую форму строчного дайджеста. Не
// разрешает пакет и не перепроверяет права субъекта.
func validSystemAssistantGrantConnectionHeader(header systemAssistantGrantConnectionHeader, connectionRef string) bool {
	return connectionRef != "" && header.ref == connectionRef && header.name != "" && header.version > 0 &&
		header.definitionKey != "" && header.definitionVersion != "" && validAssistantDefinitionDigest(header.definitionDigest)
}

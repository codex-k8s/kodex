package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/system_assistant_grant__header.sql
var querySystemAssistantGrantHeader string

type systemAssistantGrantConnectionHeader struct {
	ref, name, definitionKey, definitionVersion, definitionDigest string
	version                                                       int64
}

// Вызывается после полного SYSTEM owner/admission path. Это private header,
// не UI connection projection и не cache authority; новые locks не добавлены.
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

func validSystemAssistantGrantConnectionHeader(header systemAssistantGrantConnectionHeader, connectionRef string) bool {
	return connectionRef != "" && header.ref == connectionRef && header.name != "" && header.version > 0 &&
		header.definitionKey != "" && header.definitionVersion != "" && validAssistantDefinitionDigest(header.definitionDigest)
}

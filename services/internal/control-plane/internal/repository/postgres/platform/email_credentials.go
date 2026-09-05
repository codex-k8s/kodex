package platform

import (
	"context"
	_ "embed"
	"errors"
	"strconv"
	"strings"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/email_credential_lock.sql
var queryEmailCredentialLock string

//go:embed sql/email_credential_insert.sql
var queryEmailCredentialInsert string

//go:embed sql/email_credential_advance_connection.sql
var queryEmailCredentialAdvanceConnection string

//go:embed sql/email_credential_digests.sql
var queryEmailCredentialDigests string

func (repository *Repository) EmailCredentialDigests(ctx context.Context, configuration api.Configuration) (map[string]string, error) {
	type owner struct{ tenant, connection, kind string }
	expected := map[string]owner{}
	for _, mailbox := range configuration.Mailboxes {
		if !mailbox.Enabled {
			continue
		}
		for _, endpoint := range []*api.Endpoint{&mailbox.Smtp, mailbox.Imap, mailbox.Pop} {
			if endpoint == nil {
				continue
			}
			for kind, descriptor := range map[string]api.Descriptor{"CA_CERTIFICATE": endpoint.Ca, "USERNAME": endpoint.Username, "AUTH_SECRET": endpoint.Secret} {
				key := descriptor.Name + "." + strconv.FormatInt(descriptor.Generation, 10)
				binding := owner{mailbox.TenantId, mailbox.ConnectionId, kind}
				if previous, ok := expected[key]; ok && previous != binding {
					return nil, errs.ErrConflict
				}
				expected[key] = binding
			}
		}
	}
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	result := make(map[string]string, len(expected))
	if len(keys) == 0 {
		return result, nil
	}
	rows, err := repository.pool.Query(ctx, queryEmailCredentialDigests, keys)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var name, digest string
		var generation int64
		var binding owner
		if err := rows.Scan(&name, &generation, &binding.kind, &digest, &binding.connection, &binding.tenant); err != nil {
			return nil, errs.ErrUnavailable
		}
		key := name + "." + strconv.FormatInt(generation, 10)
		if expected[key] != binding {
			return nil, errs.ErrConflict
		}
		result[key] = digest
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	if len(result) != len(expected) {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) configureEmailCredential(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.EmailCredentialInput)
	if !ok || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	if payload.ReplayOnly {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	credential := payload.Credential
	if !strings.HasPrefix(credential.Name, "email-") || len(credential.Name) != 38 || !validRuntimeSecretSHA256(credential.ContentSHA256) ||
		credential.Generation != *input.Mutation.ExpectedVersion+1 || credential.SecretUID == "" || credential.SecretResourceVersion == "" ||
		!strings.HasSuffix(credential.SecretRef, "/email-bridge-mailbox-projection#"+credential.Name+"."+strconv.FormatInt(credential.Generation, 10)) {
		return commandOutcome{}, errs.ErrInvalid
	}
	switch credential.Kind {
	case "CA_CERTIFICATE", "USERNAME", "AUTH_SECRET":
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
	var connectionID string
	var version int64
	err := tx.QueryRow(ctx, queryEmailCredentialLock, current.organizationID, payload.ConnectionRef).Scan(&connectionID, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	_, err = tx.Exec(ctx, queryEmailCredentialInsert, pgx.StrictNamedArgs{
		"name": credential.Name, "generation": credential.Generation, "organization_id": current.organizationID, "connection_id": connectionID,
		"kind": credential.Kind, "content_sha256": credential.ContentSHA256, "secret_ref": credential.SecretRef, "secret_uid": credential.SecretUID,
		"secret_resource_version": credential.SecretResourceVersion, "actor_id": current.actorID,
	})
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if err := tx.QueryRow(ctx, queryEmailCredentialAdvanceConnection, connectionID, current.organizationID, version).Scan(&credential.ConnectionVersion); err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	credential.ConnectionRef = payload.ConnectionRef
	return commandOutcome{result: command.Result{EmailCredential: &credential}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: payload.ConnectionRef,
		summary: "i18n:EMAIL_MAILBOX_CREDENTIAL_CREATED", platformEvent: "INTEGRATION_CONNECTION_CHANGED"}, nil
}

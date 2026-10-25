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

// queryEmailCredentialLock embeds a FOR UPDATE lookup of one same-tenant email connection whose
// state is not DELETED. It returns connection UUID/version for credential OCC; the caller
// separately validates the command and authorization.
// queryEmailCredentialLock встраивает FOR UPDATE lookup одной email connection того же tenant со
// state, отличным от DELETED. Возвращает connection UUID/version для credential OCC; caller
// отдельно проверяет command и authorization.
//
//go:embed sql/email_credential_lock.sql
var queryEmailCredentialLock string

// queryEmailCredentialInsert embeds insertion of immutable credential descriptor metadata with
// exact name/generation, organization/connection, kind, content digest, Secret
// locator/UID/resourceVersion and actor. It stores a descriptor rather than reading or copying
// secret bytes.
// queryEmailCredentialInsert встраивает добавление immutable credential descriptor metadata с
// точными name/generation, organization/connection, kind, content digest, Secret
// locator/UID/resourceVersion и actor. Сохраняет descriptor вместо чтения или копирования secret
// bytes.
//
//go:embed sql/email_credential_insert.sql
var queryEmailCredentialInsert string

// queryEmailCredentialAdvanceConnection embeds the version-checked connection increment returning
// the new version under exact UUID/organization/current-version. It is part of the caller
// credential transaction, not an independent commit.
// queryEmailCredentialAdvanceConnection встраивает version-checked увеличение connection version с
// возвратом новой version внутри точных UUID/organization/current-version. Является частью caller
// credential transaction, а не отдельным commit.
//
//go:embed sql/email_credential_advance_connection.sql
var queryEmailCredentialAdvanceConnection string

// queryEmailCredentialDigests embeds descriptor name/generation/kind/digest and owning
// connection/organization for supplied composite keys. The SQL itself has no actor/tenant filter;
// the private caller must validate every returned owner tuple against its trusted configuration.
// queryEmailCredentialDigests встраивает descriptor name/generation/kind/digest и owning
// connection/organization для переданных composite keys. Сам SQL не содержит actor/tenant filter;
// private caller обязан сверить каждый возвращённый owner tuple с доверенной configuration.
//
//go:embed sql/email_credential_digests.sql
var queryEmailCredentialDigests string

// EmailCredentialDigests delegates descriptor-digest collection through the repository pool for
// the supplied configuration. It returns only digest metadata, adds no transaction or authority
// check and relies on the collector owner/cardinality validation.
// EmailCredentialDigests делегирует сбор descriptor digests через repository pool для переданной
// configuration. Возвращает только digest metadata, не добавляет transaction или authority check и
// полагается на owner/cardinality validation сборщика.
func (repository *Repository) EmailCredentialDigests(ctx context.Context, configuration api.Configuration) (map[string]string, error) {
	return emailCredentialDigests(ctx, repository.pool, configuration)
}

// emailCredentialQuerier is the narrow Query port used to read descriptor digest rows from a pool
// or fixture. It exposes no mutation method or transaction ownership; returned row iteration
// belongs to the collector.
// emailCredentialQuerier является узким Query port чтения descriptor digest rows из pool либо
// fixture. Не предоставляет mutation method или владение transaction; iteration возвращённых rows
// принадлежит сборщику.
type emailCredentialQuerier interface {
	// Query executes the supplied parameterized metadata query and returns pgx rows/error unchanged.
	// The consumer must close rows and check iteration errors; this interface method establishes no
	// resource authority.
	// Query выполняет переданный parameterized metadata query и возвращает pgx rows/error без
	// изменения. Consumer обязан закрыть rows и проверить iteration errors; interface method не
	// устанавливает resource authority.
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// emailCredentialDigests collects CA/username/auth descriptors from enabled mailbox endpoints,
// rejecting one name.generation reused across different tenant/connection/kind tuples. It reads
// metadata once, checks returned owners and full cardinality, closes rows and maps
// query/scan/iteration failures to Unavailable; missing/mismatched descriptors are Conflict. Empty
// input returns a nonnil map. Configuration shapes and digest grammar are not validated here, and
// secret payloads are never loaded.
// emailCredentialDigests собирает CA/username/auth descriptors enabled mailbox endpoints, отклоняя
// повтор name.generation для разных tenant/connection/kind tuples. Один раз читает metadata,
// сверяет returned owners и полную cardinality, закрывает rows и отображает query/scan/iteration
// failures в Unavailable; отсутствующие/несовпадающие descriptors дают Conflict. Пустой input
// возвращает ненулевую map. Configuration shapes и digest grammar здесь не проверяются, secret
// payloads не загружаются.
func emailCredentialDigests(ctx context.Context, querier emailCredentialQuerier, configuration api.Configuration) (map[string]string, error) {
	// owner identifies the expected tenant, connection and credential kind for one descriptor
	// composite key. It is local comparison data derived from the trusted configuration, not caller
	// identity or a secret value.
	// owner задаёт ожидаемые tenant, connection и credential kind одного descriptor composite key. Это
	// локальные comparison data из доверенной configuration, а не caller identity или secret value.
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
	rows, err := querier.Query(ctx, queryEmailCredentialDigests, keys)
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
		return nil, errs.ErrConflict
	}
	return result, nil
}

// configureEmailCredential requires the specialized payload and expected version, rejects
// replay-only input and validates the descriptor prefix/length, digest, generation, Secret
// UID/resourceVersion/suffix and closed kind. It locks the same-tenant email connection, enforces
// OCC, inserts metadata and advances the connection version in the caller transaction. Write
// errors retain mapWriteError; the result includes the connection-change event intent. It neither
// materializes secret bytes nor commits, and current permission/receipt handling belongs to the
// command path.
// configureEmailCredential требует specialized payload и expected version, отклоняет replay-only
// input и проверяет descriptor prefix/length, digest, generation, Secret
// UID/resourceVersion/suffix и закрытый kind. Блокирует email connection того же tenant, требует
// OCC, добавляет metadata и увеличивает connection version в caller transaction. Write errors
// сохраняют mapWriteError; результат содержит connection-change event intent. Не материализует
// secret bytes и не выполняет commit; current permission/receipt handling принадлежит command
// path.
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

package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// emailHealthProbeTx exercises actual TestConnection admission up to its first write, which
// deliberately fails. It supplies no database, mailbox credentials or successful health result.
// emailHealthProbeTx проверяет реальный TestConnection до первой записи, намеренно завершаемой
// ошибкой. Оснастка не предоставляет БД, credentials mailbox или успешный health result.
type emailHealthProbeTx struct {
	pgx.Tx
	t                                           *testing.T
	definition                                  integrationpackage.Package
	policy                                      entity.IntegrationCredentialPolicy
	active, configured, foreign, genericToken   bool
	ownerReads, setupReads, sourceReads, writes int
}

// emailHealthProbeRows represents empty dependent work without authorizing its cancellation.
// emailHealthProbeRows представляет отсутствие зависимой работы без выдачи права её отмены.
type emailHealthProbeRows struct{ pgx.Rows }

// Next reports no dependency rows for this admission-only fixture.
// Next сообщает отсутствие dependency rows в оснастке только для проверки допуска.
func (emailHealthProbeRows) Next() bool { return false }

// Close has no resources to release in this empty fixture.
// Close не освобождает ресурсы в этой пустой оснастке.
func (emailHealthProbeRows) Close() {}

// Err preserves the successful empty dependency observation.
// Err сохраняет успешное наблюдение отсутствия зависимостей.
func (emailHealthProbeRows) Err() error { return nil }

// Query allows only the two existing dependency scans before the health admission boundary.
// Query допускает только два существующих dependency scans перед границей health admission.
func (tx *emailHealthProbeTx) Query(_ context.Context, statement string, _ ...any) (pgx.Rows, error) {
	if statement != queryWriteBackActive && statement != queryRepositoryBundleDependency {
		tx.t.Fatal("unexpected dependency query")
	}
	return emailHealthProbeRows{}, nil
}

// emailHealthProbeRow delegates exact scalar fixture assignments without interpreting SQL.
// emailHealthProbeRow передаёт точные scalar fixture assignments без интерпретации SQL.
type emailHealthProbeRow func([]any) error

// Scan applies the single query's fixed fixture projection.
// Scan применяет фиксированную fixture projection одного запроса.
func (row emailHealthProbeRow) Scan(destinations ...any) error { return row(destinations) }

// QueryRow supplies locked connection/current owner proofs and stops before the state write.
// QueryRow задаёт locked connection/current owner proofs и останавливается перед записью state.
func (tx *emailHealthProbeTx) QueryRow(_ context.Context, statement string, arguments ...any) pgx.Row {
	return emailHealthProbeRow(func(d []any) error {
		switch statement {
		case queryConfigurationChangeconnectionLock:
			*d[0].(*string), *d[1].(*string) = "connection_id", tx.definition.Metadata.Key
			*d[2].(*string), *d[3].(*string), *d[4].(*bool) = "ACTIVE", "NOT_CONNECTED", true
			*d[5].(*int64), *d[6].(*string), *d[7].(*string), *d[8].(*entity.IntegrationCredentialPolicy) = 1, tx.definition.Metadata.Version, tx.definition.Digest, tx.policy
		case queryEmailCredentialOwnerRead:
			tx.ownerReads++
			*d[0].(*string), *d[1].(*int64), *d[2].(*string), *d[3].(*bool), *d[4].(*entity.IntegrationCredentialPolicy) = "connection_id", 1, "NOT_CONNECTED", true, tx.policy
			*d[5].(*string), *d[6].(*string), *d[7].(*string), *d[8].(*bool) = "owner_id", "subject_owner", "USER", tx.active
			if tx.policy == entity.IntegrationCredentialProjectShared {
				*d[5].(*string), *d[7].(*string) = "service_id", "SERVICE"
			}
			*d[9].(*string), *d[10].(*int64), *d[11].(*string) = "actor_id", 1, "ACTIVE"
			if tx.foreign {
				*d[9].(*string) = "other_actor_id"
			}
			*d[12].(*string), *d[13].(*string), *d[14].(*bool) = "actor_id", "USER", true
		case queryEmailConnectionTestSetup:
			tx.setupReads++
			*d[0].(*bool) = tx.configured
		case querySSHComposeActiveDependency:
			*d[0].(*bool) = false
		case querySharedHealthCredentialSource:
			tx.sourceReads++
			if len(arguments) != 1 {
				tx.t.Fatal("missing health delivery query arguments")
			}
			args, ok := arguments[0].(pgx.StrictNamedArgs)
			if !ok || len(args) != 3 || args["organization_id"] != "organization_id" || args["connection_id"] != "connection_id" || args["allow_personal_email"] != true {
				tx.t.Fatal("health delivery was not bound to the checked EMAIL owner")
			}
			*d[0].(*[]byte), *d[1].(*bool) = []byte("{}"), tx.genericToken
			*d[2].(*string), *d[3].(*string), *d[4].(*int64) = "", "", 0
			*d[5].(*entity.IntegrationBrokerIdentityPurpose), *d[6].(*string) = "", ""
			*d[7].(**time.Time), *d[8].(*string), *d[9].(*bool) = nil, "", false
		case queryConfigurationChangeconnectionUpdateIntegrationConnectionsStateLastTestSummaryVersion:
			tx.writes++
			return errs.ErrUnavailable
		default:
			tx.t.Fatal("unexpected admission query")
		}
		return nil
	})
}

// TestEmailHealthCommandRequiresPublishedCurrentOwner covers the actual personal/shared health
// consumer admission, disabled/foreign owners and unfinished setup. Reaching its denied first
// write is not a PostgreSQL create, queued test or SMTP/IMAP success.
// TestEmailHealthCommandRequiresPublishedCurrentOwner проверяет реальный допуск personal/shared
// health consumer, отключённого/чужого владельца и незавершённую настройку. Достижение намеренно
// отклонённой первой записи не доказывает PostgreSQL create, очередь test либо успех SMTP/IMAP.
func TestEmailHealthCommandRequiresPublishedCurrentOwner(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name                                      string
		policy                                    entity.IntegrationCredentialPolicy
		active, configured, foreign, genericToken bool
		want                                      error
		writes, setupReads, sourceReads           int
	}{
		{"personal", entity.IntegrationCredentialInitiatorRequired, true, true, false, false, errs.ErrUnavailable, 1, 1, 1},
		{"explicit-service", entity.IntegrationCredentialProjectShared, true, true, false, false, errs.ErrUnavailable, 1, 1, 1},
		{"generic-token-residue", entity.IntegrationCredentialInitiatorRequired, true, true, false, true, errs.ErrUnavailable, 0, 1, 1},
		{"unpublished", entity.IntegrationCredentialInitiatorRequired, true, false, false, false, errs.ErrConflict, 0, 1, 0},
		{"owner-disabled", entity.IntegrationCredentialInitiatorRequired, false, true, false, false, errs.ErrForbidden, 0, 0, 0},
		{"foreign-configurer", entity.IntegrationCredentialInitiatorRequired, true, true, true, false, errs.ErrNotFound, 0, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &emailHealthProbeTx{t: t, definition: definitions["email"], policy: test.policy, active: test.active, configured: test.configured, foreign: test.foreign, genericToken: test.genericToken}
			repository := &Repository{integrationDefinitions: definitions}
			version := int64(1)
			_, err := repository.changeConnection(t.Context(), tx, scope{organizationID: "organization_id", actorID: "actor_id", actorRef: "subject_actor"}, command.Command{Kind: command.TestConnection, Mutation: value.Mutation{ExpectedVersion: &version}, Payload: command.ConnectionInput{Ref: "int_mailbox"}})
			if !errors.Is(err, test.want) || tx.ownerReads != 1 || tx.setupReads != test.setupReads || tx.sourceReads != test.sourceReads || tx.writes != test.writes {
				t.Fatalf("health admission mismatch: err=%v owners=%d setup=%d source=%d writes=%d", err, tx.ownerReads, tx.setupReads, tx.sourceReads, tx.writes)
			}
		})
	}
}

package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// integrationClaimTestTx records only the synthetic claim query.
// integrationClaimTestTx фиксирует только запрос проверки синтетического захвата.
type integrationClaimTestTx struct {
	pgx.Tx
	query   string
	args    []any
	allowed bool
	scanErr error
}

// QueryRow records argument order and supplies a controlled Scan result.
// QueryRow сохраняет порядок аргументов и возвращает заданный результат Scan.
func (tx *integrationClaimTestTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.query, tx.args = query, append([]any(nil), args...)
	return integrationClaimTestRow{allowed: tx.allowed, err: tx.scanErr}
}

// integrationClaimTestRow models an allowed, denied or failed read.
// integrationClaimTestRow воспроизводит успешный ответ, запрет и ошибку чтения.
type integrationClaimTestRow struct {
	allowed bool
	err     error
}

// Scan rejects unexpected destination types or column counts.
// Scan не разрешает неожиданные типы или количество колонок.
func (row integrationClaimTestRow) Scan(destinations ...any) error {
	if len(destinations) != 1 {
		panic("unexpected integration claim scan")
	}
	result, ok := destinations[0].(*bool)
	if !ok {
		panic("unexpected integration claim destination")
	}
	*result = row.allowed
	return row.err
}

// TestIntegrationClaimAuthority preserves distinct commands, exact pins and failures.
// TestIntegrationClaimAuthority сохраняет разные команды, точные аргументы и отказы.
func TestIntegrationClaimAuthority(t *testing.T) {
	t.Parallel()
	testPayload := command.IntegrationConnectionTestInput{TestRef: "test", LeaseRef: "lease", Fence: "synthetic-fence", Generation: 7}
	invocationPayload := command.IntegrationInvocationInput{InvocationRef: "invocation", LeaseRef: "lease", Fence: "synthetic-fence", Generation: 7}
	digest := sha256.Sum256([]byte("synthetic-fence"))
	for _, test := range []struct {
		name    string
		query   string
		ref     string
		payload any
		other   any
		check   func(context.Context, pgx.Tx, scope, command.Command) error
	}{
		{"test", queryIntegrationTestAuthorizeCompletion, "test", testPayload, invocationPayload, (&Repository{}).authorizeIntegrationTestCompletion},
		{"invocation", queryIntegrationInvocationAuthorizeCompletion, "invocation", invocationPayload, testPayload, (&Repository{}).authorizeIntegrationCompletion},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, outcome := range []struct {
				name          string
				allowed       bool
				scanErr, want error
			}{
				{name: "allow", allowed: true},
				{name: "deny", want: errs.ErrForbidden},
				{name: "read failure", scanErr: errors.New("synthetic failure"), want: errs.ErrUnavailable},
			} {
				t.Run(outcome.name, func(t *testing.T) {
					tx := &integrationClaimTestTx{allowed: outcome.allowed, scanErr: outcome.scanErr}
					input := command.Command{Principal: value.Principal{CallerWorkload: "integration-gateway"}, Payload: test.payload}
					err := test.check(t.Context(), tx, scope{organizationID: "organization"}, input)
					wantArgs := []any{"organization", test.ref, "integration-gateway", "lease", hex.EncodeToString(digest[:]), int64(7)}
					if !errors.Is(err, outcome.want) || tx.query != test.query || !reflect.DeepEqual(tx.args, wantArgs) {
						t.Fatal("unexpected claim authority outcome or query pins")
					}
				})
			}
			for _, invalid := range []struct {
				input command.Command
				want  error
			}{
				{command.Command{Principal: value.Principal{CallerWorkload: "unknown"}, Payload: test.payload}, errs.ErrForbidden},
				{command.Command{Principal: value.Principal{CallerWorkload: "integration-gateway"}, Payload: test.other}, errs.ErrInvalid},
			} {
				tx := &integrationClaimTestTx{}
				err := test.check(t.Context(), tx, scope{organizationID: "organization"}, invalid.input)
				if !errors.Is(err, invalid.want) || tx.query != "" {
					t.Fatal("invalid workload or payload reached SQL")
				}
			}
		})
	}
}

package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// interactionReceiptExecTx accepts only the exact receipt CTE, request context and named arguments
// expected by a test. Its nil embedded transaction rejects any unexpected query, commit or gate
// transition; generated receipt identity is checked structurally without exposing it.
// interactionReceiptExecTx принимает только точные CTE квитанции, контекст и именованные аргументы
// теста. Встроенная nil транзакция отклоняет неожиданный запрос, commit или переход шлюза;
// создаваемая ссылка квитанции проверяется по форме без её вывода.
type interactionReceiptExecTx struct {
	pgx.Tx
	t     *testing.T
	ctx   context.Context
	args  pgx.StrictNamedArgs
	cause error
	calls int
}

// Exec checks every owner/identity/digest/external-coordinate argument and a newly generated irc
// reference before returning the configured driver outcome. Only the generated reference is
// excluded from literal equality; no external effect or implicit commit is implemented.
// Exec проверяет все аргументы владельца, личности, хешей и внешних координат, новую ссылку irc,
// затем возвращает заданный исход драйвера. Только создаваемая ссылка исключена из буквального
// сравнения; внешний эффект и неявный commit не реализованы.
func (tx *interactionReceiptExecTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tx.t.Helper()
	tx.calls++
	if ctx != tx.ctx || query != queryInteractionInsertMessageReceipt || len(args) != 1 {
		tx.t.Fatal("receipt changed context, SQL or argument shape")
	}
	named, ok := args[0].(pgx.StrictNamedArgs)
	if !ok {
		tx.t.Fatal("receipt requires strict named arguments")
	}
	ref, ok := named["receipt_ref"].(string)
	if !ok || !strings.HasPrefix(ref, "irc_") || len(ref) <= 4 {
		tx.t.Fatal("receipt reference is missing or malformed")
	}
	want := make(pgx.StrictNamedArgs, len(tx.args)+1)
	for key, value := range tx.args {
		want[key] = value
	}
	want["receipt_ref"] = ref
	if !reflect.DeepEqual(named, want) {
		tx.t.Fatal("receipt changed owner, identity, decision or external coordinates")
	}
	return pgconn.NewCommandTag("INSERT 0 1"), tx.cause
}

// TestStaleInteractionGateOutcomePreservesReceiptAndResponse checks explicit and fallback root
// posts with successful and failed writes. Exact trusted subject/identity and receipt metadata
// reach the CTE once, decision/caller receipt stay unchanged, and a matched STALE response is
// returned only after success. Driver details are hidden; the fixture rejects other DB actions.
// TestStaleInteractionGateOutcomePreservesReceiptAndResponse проверяет явное и запасное корневое
// сообщение при успешной и неуспешной записи. Точные доверенные субъект, личность и метаданные
// передаются CTE однократно; решение и исходная квитанция не меняются. Ответ найденного STALE
// шлюза возвращается только при успехе, детали драйвера скрываются; прочие DB-действия запрещены.
func TestStaleInteractionGateOutcomePreservesReceiptAndResponse(t *testing.T) {
	driverFailure := errors.New("synthetic private driver detail")
	for _, test := range []struct {
		name, root, wantRoot string
		cause, wantErr       error
	}{
		{name: "explicit-root", root: "root-external", wantRoot: "root-external"},
		{name: "post-fallback", wantRoot: "post-external"},
		{name: "explicit-root-write-failure", root: "root-external", wantRoot: "root-external", cause: driverFailure, wantErr: errs.ErrUnavailable},
		{name: "fallback-write-failure", wantRoot: "post-external", cause: driverFailure, wantErr: errs.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := scope{organizationID: "organization-internal", actorID: "human-internal", interactionIdentityID: "identity-internal"}
			message := command.InteractionMessageInput{ExternalTeamRef: "team-external", ExternalChannelRef: "channel-external", ExternalRootPostRef: test.root, ExternalPostRef: "post-external"}
			receipt := interactionReceipt{connectionID: "connection-internal", grantID: "grant-internal", gateID: "gate-internal", rootRunRef: "run-public", projectID: "project-internal", eventDigest: "event-digest", userDigest: "user-digest", decision: "APPROVE", outcome: "caller-original"}
			original := receipt
			tx := &interactionReceiptExecTx{t: t, ctx: t.Context(), cause: test.cause, args: pgx.StrictNamedArgs{
				"external_team_ref": "team-external", "external_channel_ref": "channel-external", "external_root_post_ref": test.wantRoot,
				"organization_id": "organization-internal", "project_id": "project-internal", "connection_id": "connection-internal",
				"grant_id": "grant-internal", "root_run_ref": "run-public", "gate_id": "gate-internal",
				"external_event_digest": "event-digest", "external_user_digest": "user-digest", "outcome": "STALE", "decision": "APPROVE",
				"identity_id": "identity-internal", "subject_id": "human-internal",
			}}
			got, matched, err := (&Repository{}).staleInteractionGateOutcome(tx.ctx, tx, current, message, receipt, "gate-public", "project-public")
			want := commandOutcome{}
			if test.wantErr == nil {
				want = commandOutcome{result: command.Result{Runtime: map[string]any{"outcome": "STALE", "messageKey": "i18n:MATTERMOST_GATE_STALE", "acceptedResourceRef": "gate-public"}}, projectID: "project-internal", projectRef: "project-public", resourceKind: "INTERACTION_MESSAGE", resourceRef: "gate-public", summary: "i18n:MATTERMOST_GATE_STALE"}
			}
			if !matched || !errors.Is(err, test.wantErr) || !reflect.DeepEqual(got, want) || tx.calls != 1 || receipt != original {
				t.Fatal("stale gate receipt changed response, write count or caller value")
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("stale gate outcome exposed driver detail")
			}
		})
	}
}

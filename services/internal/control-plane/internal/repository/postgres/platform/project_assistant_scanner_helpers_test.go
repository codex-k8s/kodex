package platform

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// TestConversationAssistantLookupsPreserveScopeAndPartialScan checks both repository entrypoints
// against their exact SQL, context and named arguments, five scan destinations and missing/wrapped
// rows. Partial output remains available on error while driver details stay hidden; neither path
// commits. Invalid creation scope fails before any transaction access.
// TestConversationAssistantLookupsPreserveScopeAndPartialScan проверяет оба входа репозитория:
// точные SQL, контекст и именованные аргументы, пять полей сканирования и отсутствие строк, включая
// обёрнутую ошибку. Частичный результат сохраняется при ошибке без раскрытия деталей драйвера;
// commit не выполняется. Неверная область создания отклоняется до обращения к транзакции.
func TestConversationAssistantLookupsPreserveScopeAndPartialScan(t *testing.T) {
	repository := &Repository{}
	current := scope{organizationID: "organization-internal", actorID: "actor-internal", authorityProjectID: "authority-project-internal"}
	full := conversationAssistant{ID: "assistant-internal", Ref: "assistant-public", Scope: "PROJECT", ProfileRef: "profile-public", ProjectRef: "project-public"}
	partial := conversationAssistant{ID: full.ID, Ref: full.Ref}
	driverFailure := errors.New("synthetic private driver detail")
	for _, creation := range []bool{true, false} {
		mode := "existing-conversation"
		if creation {
			mode = "creation-candidate"
		}
		t.Run(mode, func(t *testing.T) {
			for _, test := range []struct {
				name           string
				fill           int
				cause, wantErr error
				want           conversationAssistant
			}{
				{name: "success", fill: 5, want: full},
				{name: "missing", cause: pgx.ErrNoRows, wantErr: errs.ErrNotFound},
				{name: "wrapped-missing", cause: errors.Join(driverFailure, pgx.ErrNoRows), wantErr: errs.ErrNotFound},
				{name: "scan-failure", cause: driverFailure, wantErr: errs.ErrUnavailable},
				{name: "partial-scan-failure", fill: 2, cause: driverFailure, wantErr: errs.ErrUnavailable, want: partial},
				{name: "partial-missing", fill: 2, cause: pgx.ErrNoRows, wantErr: errs.ErrNotFound, want: partial},
			} {
				t.Run(test.name, func(t *testing.T) {
					tx := &proofResolutionTx{t: t, ctx: t.Context()}
					if creation {
						tx.query = queryProjectAssistantResolveCandidate
						tx.args = []any{pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ref": "project-public", "assistant_scope": "PROJECT", "authority_project": current.authorityProjectID}}
					} else {
						tx.query = queryProjectAssistantResolveConversation
						tx.args = []any{pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "conversation_ref": "conversation-public", "authority_project": current.authorityProjectID}}
					}
					tx.row = proofResolutionRow(func(dest ...any) error {
						if len(dest) != 5 {
							t.Fatal("assistant lookup changed scan projection")
						}
						for i, item := range []string{full.ID, full.Ref, full.Scope, full.ProfileRef, full.ProjectRef} {
							if i < test.fill {
								*dest[i].(*string) = item
							}
						}
						return test.cause
					})
					var got conversationAssistant
					var err error
					if creation {
						got, err = repository.assistantForConversationCreation(tx.ctx, tx, current, command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: "project-public"})
					} else {
						got, err = repository.conversationAssistantTx(tx.ctx, tx, current, "conversation-public")
					}
					if !errors.Is(err, test.wantErr) || !reflect.DeepEqual(got, test.want) {
						t.Fatal("assistant lookup changed identity or error mapping")
					}
					if tx.queryCalls != 1 || tx.commitCalls != 0 {
						t.Fatal("assistant lookup changed transaction boundary")
					}
					if err != nil && strings.Contains(err.Error(), "private") {
						t.Fatal("assistant lookup exposed driver detail")
					}
				})
			}
		})
	}
	for _, input := range []command.AssistantConversationInput{{}, {AssistantScope: "UNKNOWN"}, {AssistantScope: "PROJECT"}} {
		got, err := repository.assistantForConversationCreation(t.Context(), nil, current, input)
		if err != errs.ErrInvalid || got != (conversationAssistant{}) {
			t.Fatal("invalid creation scope reached transaction or changed result")
		}
	}
}

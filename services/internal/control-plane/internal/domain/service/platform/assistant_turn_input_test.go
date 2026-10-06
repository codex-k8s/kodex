package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type assistantTurnInputRepository struct {
	platformrepo.Repository
	calls   int
	content string
}

func (*assistantTurnInputRepository) ResolvePrincipal(_ context.Context, principal value.Principal) (value.Principal, error) {
	return principal, nil
}
func (repository *assistantTurnInputRepository) Execute(_ context.Context, input command.Command) (command.Result, error) {
	repository.calls++
	repository.content = input.Payload.(command.AssistantTurnInput).Content
	return command.Result{}, nil
}

func TestAssistantTurnInputValidatedBeforeOwnerTransaction(t *testing.T) {
	for _, test := range []struct {
		name, content string
		valid         bool
	}{
		{"russian-live-size", strings.Repeat("я", 24995), true},
		{"russian-boundary", strings.Repeat("я", 32768), true},
		{"overflow", strings.Repeat("я", 32769), false},
		{"invalid-utf8", "bad\xff", false},
		{"nul", "bad\x00", false},
		{"empty", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &assistantTurnInputRepository{}
			service, err := New(repository)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Execute(t.Context(), command.Command{Kind: command.AddAssistantTurn, Principal: providerTestPrincipal(), Mutation: value.Mutation{IdempotencyKey: "turn-boundary"}, Payload: command.AssistantTurnInput{ConversationRef: "cnv_fixture", Content: test.content, DeliveryMode: "QUEUE"}})
			if test.valid {
				if err != nil || repository.calls != 1 || repository.content != test.content {
					t.Fatal("valid input changed before owner transaction")
				}
			} else if !errors.Is(err, errs.ErrInvalid) || repository.calls != 0 {
				t.Fatal("invalid input reached owner transaction")
			}
		})
	}
}

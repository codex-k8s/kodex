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

// assistantTurnInputRepository counts owner transaction commands and captures their synthetic
// assistant-turn content without persisting a conversation.
// assistantTurnInputRepository подсчитывает команды транзакции владельца и сохраняет синтетический
// текст turn помощника без сохранения диалога.
type assistantTurnInputRepository struct {
	platformrepo.Repository
	calls   int
	content string
}

// ResolvePrincipal returns the supplied synthetic assistant-turn principal unchanged to isolate
// input validation before persistence.
// ResolvePrincipal возвращает переданный синтетический principal turn помощника без изменений для
// отдельной проверки ввода до хранения.
func (*assistantTurnInputRepository) ResolvePrincipal(_ context.Context, principal value.Principal) (value.Principal, error) {
	return principal, nil
}

// Execute counts a validated assistant-turn command and stores its exact content in the local
// fixture.
// Execute подсчитывает проверенную команду turn помощника и сохраняет её точный текст в локальной
// оснастке.
func (repository *assistantTurnInputRepository) Execute(_ context.Context, input command.Command) (command.Result, error) {
	repository.calls++
	repository.content = input.Payload.(command.AssistantTurnInput).Content
	return command.Result{}, nil
}

// TestAssistantTurnInputValidatedBeforeOwnerTransaction checks exact valid Cyrillic input through
// the 32768-rune boundary and rejects overflow, invalid UTF-8, NUL and empty content before owner
// execution.
// TestAssistantTurnInputValidatedBeforeOwnerTransaction проверяет точный корректный кириллический
// ввод до границы 32768 Unicode-символов и отказ переполнению, неверному UTF-8, NUL и пустому
// тексту до исполнения владельцем.
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

package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// connectionNameProbeTx distinguishes authorizer admission from the real Create consumer's first
// storage boundary, without providing a database, creating a connection or inventing an approval.
// connectionNameProbeTx отличает допуск authorizer от первой storage boundary реального Create
// consumer без БД, создания подключения либо выдуманного approval.
type connectionNameProbeTx struct {
	pgx.Tx
	queries int
}

// QueryRow accepts only the two exact owner/definition reads needed to observe name admission.
// QueryRow допускает лишь два точных owner/definition reads для наблюдения допуска имени.
func (tx *connectionNameProbeTx) QueryRow(_ context.Context, statement string, _ ...any) pgx.Row {
	tx.queries++
	return connectionNameProbeRow{actor: statement == queryEmailPersonalActor}
}

// connectionNameProbeRow returns the active fixture actor or a deliberate unavailable storage
// boundary; it cannot fake a successful connection write.
// connectionNameProbeRow возвращает активного fixture actor либо намеренную unavailable storage
// boundary; успешная запись подключения не имитируется.
type connectionNameProbeRow struct{ actor bool }

// Scan supplies the exact actor proof for the authorizer and denies the later storage operation.
// Scan задаёт точное actor proof для authorizer и отклоняет последующую storage operation.
func (row connectionNameProbeRow) Scan(destinations ...any) error {
	if row.actor {
		*destinations[0].(*string) = "subject_owner"
		return nil
	}
	return errs.ErrUnavailable
}

// TestPersonalEmailUnicodeNameReachesSharedCreateConsumer reproduces the former runes/bytes
// disagreement across both actual functions. Valid non-ASCII names reach storage; invalid names
// stop before any owner query. This unit proof does not replace the native PostgreSQL create test.
// TestPersonalEmailUnicodeNameReachesSharedCreateConsumer воспроизводит прежнее расхождение
// runes/bytes через обе реальные функции. Допустимые non-ASCII имена доходят до storage;
// неверные останавливаются до owner query. Unit proof не заменяет native PostgreSQL create test.
func TestPersonalEmailUnicodeNameReachesSharedCreateConsumer(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	repository := &Repository{integrationDefinitions: definitions}
	for _, test := range []struct {
		name, value string
		valid       bool
	}{
		{"cyrillic81", strings.Repeat("я", 81), true},
		{"cyrillic160", strings.Repeat("я", 160), true},
		{"emoji160", strings.Repeat("😀", 160), true},
		{"combining160", strings.Repeat("е\u0301", 80), true},
		{"cyrillic161", strings.Repeat("я", 161), false},
		{"emoji161", strings.Repeat("😀", 161), false},
		{"invalidUTF8", string([]byte{0xff}), false},
		{"newline", "name\nother", false},
		{"blank", "   ", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &connectionNameProbeTx{}
			current := scope{actorID: "subject_owner"}
			input := command.Command{Kind: command.CreatePersonalEmailMailboxConnection, Principal: value.Principal{CallerWorkload: "control-api-gateway", Permission: "platform.email-mailboxes.mine.create"}, Payload: command.PersonalEmailMailboxConnectionInput{Name: test.value, Sender: "owner@example.test"}}
			err := authorizePersonalEmailConnectionTx(context.Background(), tx, current, input)
			if !test.valid {
				if !errors.Is(err, errs.ErrInvalid) || tx.queries != 0 {
					t.Fatal("invalid Unicode name reached owner query")
				}
				return
			}
			if err != nil || tx.queries != 1 {
				t.Fatalf("valid authorizer name rejected: %v", err)
			}
			input.Kind = command.CreateConnection
			input.Payload = command.ConnectionInput{DefinitionKey: "email", Name: test.value}
			_, err = repository.changeConnection(context.Background(), tx, current, input)
			if !errors.Is(err, errs.ErrUnavailable) || tx.queries != 2 {
				t.Fatalf("valid name failed before shared Create storage: %v", err)
			}
		})
	}
	if value, ok := normalizedIntegrationConnectionName("  Ящик  "); !ok || value != "Ящик" {
		t.Fatal("connection display normalization changed")
	}
}

package platform

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// platformEventOutboxFixture embeds an unimplemented transaction while providing sequence and
// outbox capture methods for one fixed organization stream. It checks producer argument binding,
// not durable broker publication, PostgreSQL atomicity or consumer processing.
// platformEventOutboxFixture встраивает нереализованную transaction, предоставляя sequence и
// outbox capture methods одного fixed organization stream. Проверяет producer argument binding, а
// не durable broker publication, PostgreSQL atomicity либо consumer processing.
type platformEventOutboxFixture struct {
	pgx.Tx
	t       *testing.T
	payload []byte
}

// QueryRow requires the exact installation-sequence UPDATE with no arguments and returns this
// fixture as its row. Unexpected query/arity fails the test; no real installation sequence is
// advanced.
// QueryRow требует exact installation-sequence UPDATE без arguments и возвращает fixture как row.
// Unexpected query/arity завершает test; реальный installation sequence не продвигается.
func (fixture *platformEventOutboxFixture) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	fixture.t.Helper()
	if query != queryCommandsEmitplatformeventUpdateInstallationPlatformSequence || len(args) != 0 {
		fixture.t.Fatal("platform producer changed sequence ownership")
	}
	return fixture
}

// Scan requires one destination and writes synthetic sequence eight into its expected int64
// pointer. It assumes the destination type and models no sequence concurrency.
// Scan требует один destination и пишет synthetic sequence восемь в ожидаемый int64 pointer.
// Полагается на destination type и не моделирует sequence concurrency.
func (fixture *platformEventOutboxFixture) Scan(dest ...any) error {
	fixture.t.Helper()
	if len(dest) != 1 {
		fixture.t.Fatal("unexpected platform sequence projection")
	}
	*dest[0].(*int64) = 8
	return nil
}

// Exec requires exact outbox SQL, five arguments and fixed subject/ordering-key/sequence pins
// before capturing payload bytes and returning one inserted row. It stores the slice without a
// copy and performs no database write or broker send.
// Exec требует exact outbox SQL, пять arguments и fixed subject/ordering-key/sequence pins до
// сохранения payload bytes и возврата одной inserted row. Хранит slice без копии, не пишет в БД и
// не отправляет broker message.
func (fixture *platformEventOutboxFixture) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	fixture.t.Helper()
	if query != queryCommandsEmitplatformeventInsertOutboxEventsEventIdOrderingKeyPayload || len(args) != 5 ||
		args[1] != "control_plane.platform.org_example0001.events" || args[2] != "platform:org_example0001" || args[3] != int64(8) {
		fixture.t.Fatal("platform producer changed exact outbox binding")
	}
	fixture.payload = args[4].([]byte)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

// TestPlatformEventSummaryUsesContractUnicodeBound checks ASCII/Cyrillic/emoji summaries at and
// above 1000 codepoints, requiring a preserved prefix/ellipsis, valid UTF-8, bounded payload and
// exact aggregate/sequence metadata. The independent 2000-character RUN truncation bound must stay
// unchanged; only a fake outbox is used.
// TestPlatformEventSummaryUsesContractUnicodeBound проверяет ASCII/Cyrillic/emoji summaries на
// границе и выше 1000 codepoints, требуя сохранения prefix/ellipsis, valid UTF-8, bounded payload
// и exact aggregate/sequence metadata. Отдельный RUN truncation bound 2000 символов остаётся
// прежним; используется только fake outbox.
func TestPlatformEventSummaryUsesContractUnicodeBound(t *testing.T) {
	for name, character := range map[string]string{"ASCII": "a", "Cyrillic": "я", "emoji": "😀"} {
		for _, count := range []int{1000, 1001, 2000} {
			t.Run(name+"/"+strconv.Itoa(count), func(t *testing.T) {
				input := strings.Repeat(character, count)
				fixture := &platformEventOutboxFixture{t: t}
				err := (&Repository{}).emitPlatformEventSnapshot(t.Context(), fixture,
					scope{organizationRef: "org_example0001", correlationRef: "corr_example001"},
					"RUN_CHANGED", "prj_example0001", "run_example0001", input, 3, "RUNNING")
				if err != nil {
					t.Fatalf("emit platform snapshot: %v", err)
				}
				var payload struct {
					OrganizationRef  string `json:"organizationRef"`
					ProjectRef       string `json:"projectRef"`
					AggregateRef     string `json:"aggregateRef"`
					AggregateVersion int64  `json:"aggregateVersion"`
					Sequence         int64  `json:"sequence"`
					Data             struct {
						Kind        string `json:"kind"`
						State       string `json:"state"`
						SafeSummary string `json:"safeSummary"`
					} `json:"data"`
				}
				if json.Unmarshal(fixture.payload, &payload) != nil || payload.OrganizationRef != "org_example0001" ||
					payload.ProjectRef != "prj_example0001" || payload.AggregateRef != "run_example0001" ||
					payload.AggregateVersion != 3 || payload.Sequence != 8 || payload.Data.Kind != "RUN" || payload.Data.State != "RUNNING" {
					t.Fatal("platform snapshot lost authoritative metadata")
				}
				want := input
				if count > 1000 {
					want = strings.Repeat(character, 999) + "…"
				}
				if payload.Data.SafeSummary != want || !utf8.ValidString(payload.Data.SafeSummary) || utf8.RuneCountInString(payload.Data.SafeSummary) != 1000 || len(fixture.payload) > 65536 {
					t.Fatal("platform summary exceeded contract Unicode bound or changed its prefix")
				}
				if truncate(input, 2000) != input {
					t.Fatal("independent RUN summary bound changed")
				}
			})
		}
	}
}

// TestPlatformEventKindsMatchContract checks the explicit event-name to aggregate-kind mapping,
// including role-image variants sharing ROLE_IMAGE_RECIPE. It validates classification only, not
// event production, schema completeness or delivery.
// TestPlatformEventKindsMatchContract проверяет явное отображение event-name в aggregate-kind,
// включая role-image variants с общим ROLE_IMAGE_RECIPE. Проверяет только classification, а не
// event production, полноту schema либо delivery.
func TestPlatformEventKindsMatchContract(t *testing.T) {
	want := map[string]string{
		"PROJECT_CHANGED":                "PROJECT",
		"AGENT_CHANGED":                  "AGENT",
		"ARTIFACT_CHANGED":               "ARTIFACT",
		"INSTRUCTIONS_PUBLISHED":         "INSTRUCTIONS",
		"WORKFLOW_CHANGED":               "WORKFLOW",
		"SCHEDULE_CHANGED":               "SCHEDULE",
		"INTEGRATION_CONNECTION_CHANGED": "INTEGRATION_CONNECTION",
		"INTEGRATION_GRANT_CHANGED":      "INTEGRATION_GRANT",
		"MEMBERSHIP_CHANGED":             "MEMBERSHIP",
		"SYSTEM_ASSISTANT_CHANGED":       "SYSTEM_ASSISTANT",
		"ROLE_IMAGE_RECIPE_CHANGED":      "ROLE_IMAGE_RECIPE",
		"ROLE_IMAGE_PROMOTION_REQUESTED": "ROLE_IMAGE_RECIPE",
		"ROLE_IMAGE_PROMOTED":            "ROLE_IMAGE_RECIPE",
		"RUNTIME_ENVIRONMENT_CHANGED":    "RUNTIME_ENVIRONMENT",
		"PROVIDER_ACCOUNT_CHANGED":       "PROVIDER_ACCOUNT",
		"PLATFORM_MEMBERSHIP_CHANGED":    "PLATFORM_MEMBERSHIP",
		"RUNTIME_SECRET_CHANGED":         "RUNTIME_SECRET",
		"MANAGED_CONFIGURATION_CHANGED":  "MANAGED_CONFIGURATION",
		"RUN_CHANGED":                    "RUN",
	}
	for eventName, kind := range want {
		if got := platformEventKind(eventName); got != kind {
			t.Errorf("event %s projected as %s, want %s", eventName, got, kind)
		}
	}
}

// TestPlatformEventKindRejectsUnknownEvent requires an unknown event name to produce an empty kind
// rather than a generic guessed aggregate. It does not authorize or publish an event.
// TestPlatformEventKindRejectsUnknownEvent требует пустой kind для неизвестного event name вместо
// угаданного generic aggregate. Не авторизует и не публикует event.
func TestPlatformEventKindRejectsUnknownEvent(t *testing.T) {
	if got := platformEventKind("UNKNOWN_CHANGED"); got != "" {
		t.Fatalf("unknown platform event projected as %s", got)
	}
}

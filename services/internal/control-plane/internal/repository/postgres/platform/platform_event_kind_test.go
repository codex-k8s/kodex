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

type platformEventOutboxFixture struct {
	pgx.Tx
	t       *testing.T
	payload []byte
}

func (fixture *platformEventOutboxFixture) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	fixture.t.Helper()
	if query != queryCommandsEmitplatformeventUpdateInstallationPlatformSequence || len(args) != 0 {
		fixture.t.Fatal("platform producer changed sequence ownership")
	}
	return fixture
}

func (fixture *platformEventOutboxFixture) Scan(dest ...any) error {
	fixture.t.Helper()
	if len(dest) != 1 {
		fixture.t.Fatal("unexpected platform sequence projection")
	}
	*dest[0].(*int64) = 8
	return nil
}

func (fixture *platformEventOutboxFixture) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	fixture.t.Helper()
	if query != queryCommandsEmitplatformeventInsertOutboxEventsEventIdOrderingKeyPayload || len(args) != 5 ||
		args[1] != "control_plane.platform.org_example0001.events" || args[2] != "platform:org_example0001" || args[3] != int64(8) {
		fixture.t.Fatal("platform producer changed exact outbox binding")
	}
	fixture.payload = args[4].([]byte)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

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

func TestPlatformEventKindRejectsUnknownEvent(t *testing.T) {
	if got := platformEventKind("UNKNOWN_CHANGED"); got != "" {
		t.Fatalf("unknown platform event projected as %s", got)
	}
}

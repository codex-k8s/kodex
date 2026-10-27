package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// TestAssistantGrantHydrationPayloadBounds rejects missing/coerced booleans, malformed or
// excessive paths and disabled grants with paths, while preserving trimmed fresh empty slices.
// It exercises the shared input boundary without granting owner authority.
// TestAssistantGrantHydrationPayloadBounds отклоняет отсутствующий или приведённый bool,
// повреждённые либо избыточные paths и paths выключенного grant, сохраняя очищенные ненулевые
// пустые срезы. Проверяет общий вход без выдачи полномочий владельца.
func TestAssistantGrantHydrationPayloadBounds(t *testing.T) {
	for _, test := range []struct {
		name          string
		patch         map[string]any
		removeEnabled bool
		valid         bool
	}{
		{"missing_paths", nil, false, true},
		{"typed_nil_paths", map[string]any{"approvalScopePaths": []string(nil)}, false, true},
		{"empty_array", map[string]any{"approvalScopePaths": []any{}}, false, true},
		{"trimmed_path", map[string]any{"approvalScopePaths": []any{" /label "}}, false, true},
		{"sixteen", map[string]any{"approvalScopePaths": strings.Split(strings.Repeat("/label,", 15)+"/label", ",")}, false, true},
		{"seventeen", map[string]any{"approvalScopePaths": strings.Split(strings.Repeat("/label,", 16)+"/label", ",")}, false, false},
		{"missing_enabled", nil, true, false},
		{"coerced_enabled", map[string]any{"enabled": "true"}, false, false},
		{"nil_paths", map[string]any{"approvalScopePaths": nil}, false, false},
		{"wrong_paths", map[string]any{"approvalScopePaths": "path"}, false, false},
		{"mixed_paths", map[string]any{"approvalScopePaths": []any{"/label", 1}}, false, false},
		{"blank_path", map[string]any{"approvalScopePaths": []string{" "}}, false, false},
		{"disabled_paths", map[string]any{"enabled": false, "approvalScopePaths": []string{"/label"}}, false, false},
		{"disabled_empty", map[string]any{"enabled": false}, false, true},
		{"missing_key", map[string]any{"capabilityKey": ""}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := map[string]any{"enabled": true, "capabilityKey": " github.repository.read ", "connectionRef": " int_owned ", "approvalPolicy": " NONE "}
			for key, value := range test.patch {
				input[key] = value
			}
			if test.removeEnabled {
				delete(input, "enabled")
			}
			payload, err := assistantGrantHydrationPayload(input)
			if test.valid {
				if err != nil || payload.ConnectionRef != "int_owned" || payload.CapabilityKey != "github.repository.read" || payload.ApprovalPolicy != "NONE" || payload.ApprovalScopePaths == nil {
					t.Fatal("valid payload or nonnil paths changed")
				}
				if test.name == "trimmed_path" && !reflect.DeepEqual(payload.ApprovalScopePaths, []string{"/label"}) {
					t.Fatal("path trimming changed")
				}
			} else if !errors.Is(err, errs.ErrInvalid) || !reflect.DeepEqual(payload, command.SystemAssistantIntegrationGrantInput{}) {
				t.Fatal("invalid payload accepted or partially returned")
			}
		})
	}
}

// TestAssistantGrantHydrationCapabilityClosed keeps capability callability, allowed policy and
// exact typed scope paths fail-closed, including disabled grants and duplicated/undeclared paths.
// Package/owner admission is deliberately supplied by the caller rather than mocked as a grant.
// TestAssistantGrantHydrationCapabilityClosed сохраняет закрытый отказ callable, разрешённой
// policy и точных typed scope paths, включая выключенный grant и повторные/необъявленные paths.
// Допуск пакета и владельца передаёт вызывающий, а не фиктивный grant теста.
func TestAssistantGrantHydrationCapabilityClosed(t *testing.T) {
	for _, test := range []struct {
		name, key, operation, policy string
		paths                        []string
		enabled, valid               bool
	}{
		{"scoped", "github.repository.read", "github.repository.read", "HUMAN_SCOPED", []string{"/label"}, true, true},
		{"missing_capability", "missing", "github.repository.read", "NONE", nil, true, false},
		{"inbound", "github.repository.read", "mattermost.inbound", "NONE", nil, true, false},
		{"unknown_policy", "github.repository.read", "github.repository.read", "UNKNOWN", nil, true, false},
		{"undeclared_path", "github.repository.read", "github.repository.read", "HUMAN_SCOPED", []string{"/foreign"}, true, false},
		{"duplicate_path", "github.repository.read", "github.repository.read", "HUMAN_SCOPED", []string{"/label", "/label"}, true, false},
		{"empty_scope", "github.repository.read", "github.repository.read", "HUMAN_SCOPED", nil, true, false},
		{"unscoped_paths", "github.repository.read", "github.repository.read", "NONE", []string{"/label"}, true, false},
		{"disabled_empty", "github.repository.read", "github.repository.read", "HUMAN_SCOPED", nil, false, true},
		{"disabled_paths", "github.repository.read", "github.repository.read", "HUMAN_SCOPED", []string{"/label"}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			capability := integrationpackage.Capability{Key: "github.repository.read", Operation: test.operation, ApprovalPolicy: "NONE", AllowedApprovalPolicies: []string{"NONE", "HUMAN_SCOPED"}, InputFields: []integrationpackage.Field{{Key: "label", Type: "STRING", MaximumLength: 32}}}
			var definition integrationpackage.Package
			definition.Spec.Capabilities = []integrationpackage.Capability{capability}
			payload := command.SystemAssistantIntegrationGrantInput{CapabilityKey: test.key, ApprovalPolicy: test.policy, ApprovalScopePaths: test.paths, Enabled: test.enabled}
			actual, err := assistantGrantHydrationCapability(definition, payload)
			if test.valid {
				if err != nil || !reflect.DeepEqual(actual, capability) {
					t.Fatal("exact capability changed")
				}
			} else if !errors.Is(err, errs.ErrInvalid) || !reflect.DeepEqual(actual, integrationpackage.Capability{}) {
				t.Fatal("invalid capability accepted or partially returned")
			}
		})
	}
}

// assistantGrantHydrationTx intercepts only QueryRow; all other transaction methods remain
// unimplemented so an unintended write/commit cannot silently pass this fixture.
// assistantGrantHydrationTx перехватывает лишь QueryRow; остальные методы транзакции не
// реализованы, чтобы незапланированная запись или Commit не прошли незаметно.
type assistantGrantHydrationTx struct {
	pgx.Tx
	query func(context.Context, string, ...any) pgx.Row
}

// QueryRow forwards the exact context, query and ordered bindings to the test callback.
// QueryRow передаёт точные context, query и упорядоченные bindings проверке теста.
func (tx assistantGrantHydrationTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return tx.query(ctx, query, args...)
}

// assistantGrantHydrationRow allows a partial scan followed by a driver error to test that the
// snapshot reader discards all partial data and preserves the public error boundary.
// assistantGrantHydrationRow допускает частичный Scan с последующей driver error для проверки
// полного отбрасывания частичных данных и сохранения публичной границы ошибок.
type assistantGrantHydrationRow struct{ scan func(...any) error }

// Scan delegates the exact destination order without coercing types or hiding errors.
// Scan передаёт точный порядок destinations без приведения типов и скрытия ошибок.
func (row assistantGrantHydrationRow) Scan(destinations ...any) error {
	return row.scan(destinations...)
}

// TestAssistantGrantPlanSnapshotBindings checks tenant/connection/assistant/capability ordering,
// exact scan types, nonnil empty paths and fail-closed NoRows/cancellation/partial driver errors.
// It proves the local query adapter rather than PostgreSQL eligibility or transaction behavior.
// TestAssistantGrantPlanSnapshotBindings проверяет порядок tenant/connection/assistant/capability,
// точные типы Scan, ненулевые пустые paths и закрытый отказ NoRows/cancel/частичной driver error.
// Доказывает локальный query adapter, а не PostgreSQL eligibility или поведение транзакции.
func TestAssistantGrantPlanSnapshotBindings(t *testing.T) {
	driverError := errors.New("synthetic scan failure")
	for _, test := range []struct {
		name      string
		scanError error
	}{
		{"present", nil}, {"no_rows", pgx.ErrNoRows}, {"cancelled", context.Canceled}, {"partial_driver", driverError},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			tx := assistantGrantHydrationTx{query: func(actual context.Context, query string, args ...any) pgx.Row {
				calls++
				if actual != ctx || query != querySystemAssistantIntegrationGrantSnapshot || !reflect.DeepEqual(args, []any{"org_owned", "connection_owned", "assistant_owned", "capability_owned"}) {
					t.Fatal("snapshot binding changed")
				}
				return assistantGrantHydrationRow{scan: func(destinations ...any) error {
					if len(destinations) != 5 {
						t.Fatal("snapshot destination count changed")
					}
					*destinations[0].(*string) = "grant_owned"
					*destinations[1].(*int64) = 7
					*destinations[2].(*string) = "NONE"
					*destinations[3].(*bool) = true
					*destinations[4].(*[]string) = []string{}
					return test.scanError
				}}
			}}
			got, err := readAssistantGrantPlanSnapshot(ctx, tx, "org_owned", "connection_owned", "assistant_owned", "capability_owned")
			if calls != 1 {
				t.Fatal("query count changed")
			}
			if test.scanError != nil {
				if !errors.Is(err, errs.ErrUnavailable) || !reflect.DeepEqual(got, assistantGrantPlanSnapshot{}) || errors.Is(err, test.scanError) {
					t.Fatal("partial snapshot or raw driver error escaped")
				}
			} else if err != nil || got.ref != "grant_owned" || got.version != 7 || got.policy != "NONE" || !got.enabled || got.paths == nil || len(got.paths) != 0 {
				t.Fatal("snapshot tuple changed")
			}
		})
	}
}

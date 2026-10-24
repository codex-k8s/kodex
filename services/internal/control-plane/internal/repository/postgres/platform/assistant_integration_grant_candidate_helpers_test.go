package platform

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// assistantGrantRowFixture carries one synthetic six-column owner row without database access.
// assistantGrantRowFixture хранит одну синтетическую шестиколоночную owner row без доступа к БД.
type assistantGrantRowFixture struct {
	key   string
	grant entity.SystemAssistantIntegrationGrantCandidate
}

// assistantGrantRowsFixture exposes only used row methods, injecting errors after population
// and requiring Close before Err to catch leaked rows and partial-result regressions.
// assistantGrantRowsFixture предоставляет используемые методы rows, внедряя ошибку после
// заполнения и требуя Close перед Err для проверки утечки rows и частичного результата.
type assistantGrantRowsFixture struct {
	pgx.Rows
	items                []assistantGrantRowFixture
	index, scanFailureAt int
	iterationErr         error
	closed               bool
}

// Next advances one synthetic row and returns false after the configured sequence is exhausted.
// Next переходит к следующей синтетической строке и возвращает false после окончания выборки.
func (r *assistantGrantRowsFixture) Next() bool { r.index++; return r.index <= len(r.items) }

// Scan fills all six exact destinations before optionally returning a synthetic driver failure.
// Scan заполняет все шесть точных приёмников до необязательной синтетической ошибки драйвера.
func (r *assistantGrantRowsFixture) Scan(destinations ...any) error {
	item := r.items[r.index-1]
	*destinations[0].(*string) = item.grant.CurrentGrantRef
	*destinations[1].(*int64) = item.grant.CurrentGrantVersion
	*destinations[2].(*string) = item.key
	*destinations[3].(*string) = item.grant.CurrentApprovalPolicy
	*destinations[4].(*[]string) = item.grant.CurrentApprovalScopePaths
	*destinations[5].(*bool) = item.grant.CurrentGrantEnabled
	if r.index == r.scanFailureAt {
		return errors.New("synthetic scan failure")
	}
	return nil
}

// Close records release of synthetic rows before the scanner inspects the final read error.
// Close отмечает освобождение синтетических rows до проверки scanner итоговой ошибки чтения.
func (r *assistantGrantRowsFixture) Close() { r.closed = true }

// Err returns the configured iteration failure only after Close, enforcing the driver contract.
// Err возвращает заданную ошибку итерации только после Close, проверяя driver contract.
func (r *assistantGrantRowsFixture) Err() error {
	if !r.closed {
		panic("rows error read before close")
	}
	return r.iterationErr
}

// TestAssistantGrantRowsPreserveOwnerColumns checks positional fields, nonnil empty maps and
// last-row-wins duplicate keys, discarding all data after scan or iteration errors.
// TestAssistantGrantRowsPreserveOwnerColumns проверяет позиционные поля, nonnil пустую map
// и приоритет последнего ключа, отбрасывая все данные при scan либо iteration error.
func TestAssistantGrantRowsPreserveOwnerColumns(t *testing.T) {
	first := entity.SystemAssistantIntegrationGrantCandidate{CurrentGrantRef: "grant_first", CurrentGrantVersion: 7, CurrentApprovalPolicy: "HUMAN_SCOPED", CurrentApprovalScopePaths: []string{"/repo"}, CurrentGrantEnabled: true}
	last := entity.SystemAssistantIntegrationGrantCandidate{CurrentGrantRef: "grant_last", CurrentGrantVersion: 9, CurrentApprovalPolicy: "DENY", CurrentApprovalScopePaths: []string{}}
	rows := &assistantGrantRowsFixture{items: []assistantGrantRowFixture{{"cap.alpha", first}, {"cap.beta", first}, {"cap.alpha", last}}}
	got, err := scanAssistantIntegrationGrants(rows)
	want := map[string]entity.SystemAssistantIntegrationGrantCandidate{"cap.alpha": last, "cap.beta": first}
	if err != nil || !rows.closed || !reflect.DeepEqual(got, want) {
		t.Fatal("owner columns or duplicate key semantics changed")
	}
	empty := &assistantGrantRowsFixture{}
	got, err = scanAssistantIntegrationGrants(empty)
	if err != nil || got == nil || len(got) != 0 || !empty.closed {
		t.Fatal("empty query semantics changed")
	}
	for _, failure := range []struct {
		scan      int
		iteration error
	}{{1, nil}, {2, nil}, {0, errors.New("synthetic read failure")}} {
		rows = &assistantGrantRowsFixture{items: []assistantGrantRowFixture{{"cap.alpha", first}, {"cap.beta", last}}, scanFailureAt: failure.scan, iterationErr: failure.iteration}
		got, err = scanAssistantIntegrationGrants(rows)
		if err != errs.ErrUnavailable || got != nil || !rows.closed {
			t.Fatal("failed scan exposed partial grants or leaked rows")
		}
	}
}

// TestAssistantCandidateCursorPreservesWire checks scope/known-field/single-document guards
// while preserving the existing last-key-wins JSON behavior without assigning cursor authority.
// TestAssistantCandidateCursorPreservesWire проверяет scope, известные поля и один документ,
// сохраняя прежнее JSON правило последнего ключа без назначения cursor полномочий.
func TestAssistantCandidateCursorPreservesWire(t *testing.T) {
	for _, test := range []struct {
		raw, scope, want string
		invalid          bool
	}{
		{`{"Scope":"ctx","After":"cap.alpha"}`, "ctx", "cap.alpha", false},
		{`{"Scope":"old","Scope":"ctx","After":"cap.alpha"}`, "ctx", "cap.alpha", false},
		{`{"Scope":"ctx","After":"cap.alpha"}`, "other", "", true},
		{`{"Scope":"ctx","After":""}`, "ctx", "", true},
		{`{"Scope":"ctx","After":"cap/alpha"}`, "ctx", "", true},
		{`{"Scope":"ctx","After":"cap.alpha","extra":true}`, "ctx", "", true},
		{`{"Scope":"ctx","After":"cap.alpha"}{}`, "ctx", "", true},
		{`not-json`, "ctx", "", true},
	} {
		got, err := assistantIntegrationCandidateAfter(base64.RawURLEncoding.EncodeToString([]byte(test.raw)), test.scope)
		if got != test.want || (err == errs.ErrInvalid) != test.invalid || (!test.invalid && err != nil) {
			t.Fatal("candidate cursor contract changed")
		}
	}
	if got, err := assistantIntegrationCandidateAfter("!", "ctx"); got != "" || err != errs.ErrInvalid {
		t.Fatal("invalid base64 accepted")
	}
	if got, err := assistantIntegrationCandidateAfter("", "ctx"); got != "" || err != nil {
		t.Fatal("empty token rejected")
	}
}

// TestAssistantCandidatePagePreservesCountAndCursor checks sorted exclusive-key pages, exact
// token bytes, pre-cursor totals, default fifty-item size and nonnil empty/exhausted pages.
// TestAssistantCandidatePagePreservesCountAndCursor проверяет сортировку и исключающий ключ,
// точные token bytes, total до cursor, размер по умолчанию50 и nonnil пустые страницы.
func TestAssistantCandidatePagePreservesCountAndCursor(t *testing.T) {
	items := []entity.SystemAssistantIntegrationGrantCandidate{}
	for _, key := range []string{"cap.charlie", "cap.alpha", "cap.bravo"} {
		items = append(items, entity.SystemAssistantIntegrationGrantCandidate{Capability: entity.IntegrationCapability{Key: key}, Reason: "RECIPIENT_UNAVAILABLE"})
	}
	selected, total, next := assistantIntegrationCandidatePage(items, 1, "ctx", "")
	expected := base64.RawURLEncoding.EncodeToString([]byte(`{"Scope":"ctx","After":"cap.alpha"}`))
	if len(selected) != 1 || selected[0].Capability.Key != "cap.alpha" || selected[0].Reason != "RECIPIENT_UNAVAILABLE" || total != 3 || next != expected {
		t.Fatal("first page changed")
	}
	after, err := assistantIntegrationCandidateAfter(next, "ctx")
	if err != nil {
		t.Fatal("own cursor rejected")
	}
	selected, total, next = assistantIntegrationCandidatePage(items, 2, "ctx", after)
	if len(selected) != 2 || selected[0].Capability.Key != "cap.bravo" || selected[1].Capability.Key != "cap.charlie" || total != 3 || next != "" {
		t.Fatal("exclusive continuation changed")
	}
	selected, total, next = assistantIntegrationCandidatePage(items, 2, "ctx", "cap.zulu")
	if selected == nil || len(selected) != 0 || total != 3 || next != "" {
		t.Fatal("exhausted page changed")
	}
	selected, total, next = assistantIntegrationCandidatePage(nil, 0, "ctx", "")
	if selected == nil || len(selected) != 0 || total != 0 || next != "" {
		t.Fatal("empty page changed")
	}
	many := []entity.SystemAssistantIntegrationGrantCandidate{}
	for i := 0; i < 51; i++ {
		many = append(many, entity.SystemAssistantIntegrationGrantCandidate{Capability: entity.IntegrationCapability{Key: fmt.Sprintf("cap.%02d", i)}})
	}
	selected, total, next = assistantIntegrationCandidatePage(many, 0, "ctx", "")
	if len(selected) != 50 || total != 51 || selected[49].Capability.Key != "cap.49" || next == "" {
		t.Fatal("default page size changed")
	}
}

// TestIntegrationCapabilityProjectionPreservesSchema checks every advertised field against a
// fixed schema/digest and copied options, rejecting unsupported fields without a partial projection.
// TestIntegrationCapabilityProjectionPreservesSchema проверяет каждое предлагаемое поле по
// фиксированной схеме и digest, копии options и отказ неверному полю без частичной проекции.
func TestIntegrationCapabilityProjectionPreservesSchema(t *testing.T) {
	cap := integrationpackage.Capability{Key: "cap.alpha", Name: "Alpha", Description: "fixture", Operation: "READ", Risk: "LOW", ApprovalPolicy: "HUMAN_SCOPED", AllowedApprovalPolicies: []string{"DENY", "HUMAN_SCOPED"}, ResourceScope: integrationpackage.ResourceScope{Kind: "REPOSITORY"}, InputFields: []integrationpackage.Field{{Key: "label", Type: "STRING", Required: true, MaximumLength: 16}}}
	schema := `{"additionalProperties":false,"properties":{"label":{"maxLength":16,"minLength":1,"type":"string"}},"required":["label"],"type":"object"}`
	digest := sha256.Sum256([]byte(schema))
	want := entity.IntegrationCapability{Key: "cap.alpha", Name: "Alpha", Description: "fixture", Operation: "READ", Risk: "LOW", ApprovalPolicy: "HUMAN_SCOPED", AllowedApprovalPolicies: []string{"DENY", "HUMAN_SCOPED"}, ResourceKind: "REPOSITORY", InputFields: []entity.IntegrationConfigurationField{{Key: "label", Label: "label", ValueType: "TEXT", MaximumLength: 16, Required: true}}, InputSchema: schema, InputSchemaSHA256: hex.EncodeToString(digest[:])}
	got, err := integrationCapabilityProjection(cap)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("capability wire projection changed")
	}
	cap.AllowedApprovalPolicies[0] = "MUTATED"
	if got.AllowedApprovalPolicies[0] != "DENY" {
		t.Fatal("approval options alias source")
	}
	empty, err := integrationCapabilityProjection(integrationpackage.Capability{})
	if err != nil || empty.AllowedApprovalPolicies == nil || empty.InputFields == nil {
		t.Fatal("empty arrays changed")
	}
	cap.InputFields[0].Type = "UNSUPPORTED"
	got, err = integrationCapabilityProjection(cap)
	if err != errs.ErrUnavailable || !reflect.DeepEqual(got, entity.IntegrationCapability{}) {
		t.Fatal("invalid schema exposed partial projection")
	}
}

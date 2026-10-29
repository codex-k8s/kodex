package platform

import (
	"context"
	"reflect"
	"testing"
	"time"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestInstructionCommandProjectionComponent exercises native instruction saves, validation,
// publication and forward rollback against an isolated database, including durable replay.
// TestInstructionCommandProjectionComponent проверяет штатные сохранение, валидацию,
// публикацию и откат инструкций в изолированной БД, включая устойчивый replay.
func TestInstructionCommandProjectionComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated instruction projection PostgreSQL")
	}
	defer pool.Close()
	r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	seedObservedCatalogFixture(t, ctx, r)
	testInstructionDraftSave(t, ctx, r)
}

// assertInstructionCommandProjection compares every returned agent field with the authoritative
// query at the same committed version; hints and instruction bindings cannot be fabricated.
// assertInstructionCommandProjection сравнивает все поля ответа с авторитетным чтением той же
// зафиксированной версии; подсказки и привязки инструкций нельзя подменять нулевыми значениями.
func assertInstructionCommandProjection(t *testing.T, ctx context.Context, service *platformservice.Service, principal value.Principal, actual *entity.Agent) {
	t.Helper()
	if actual == nil {
		t.Fatal("instruction command omitted its agent projection")
	}
	expected, err := service.GetAgent(ctx, principal, actual.Ref)
	if err != nil {
		t.Fatal("read authoritative instruction agent", err)
	}
	if !reflect.DeepEqual(*actual, expected) {
		t.Fatalf("instruction command differs from authoritative agent: version=%d expectedVersion=%d enabled=%t expectedEnabled=%t actions=%v expectedActions=%v draftPresent=%t expectedDraftPresent=%t", actual.Version, expected.Version, actual.Enabled, expected.Enabled, actual.NextActions, expected.NextActions, actual.DraftInstructions != nil, expected.DraftInstructions != nil)
	}
}

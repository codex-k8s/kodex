package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// TestRuntimeSecretDraftReadinessComponent revokes each required privilege in an isolated
// migrated database, checks the actual owner readiness path and restores the exact grant.
// It also rejects an absent operation index, cancellation and an unauthorized principal;
// no production database, shared role membership or external materialization is modified.
// TestRuntimeSecretDraftReadinessComponent отзывает каждое обязательное право в отдельной
// мигрированной БД, проверяет настоящий путь готовности владельца и восстанавливает grant.
// Также отклоняет отсутствие индекса операций, отмену и неавторизованного участника;
// production-БД, членство общих ролей и внешняя материализация не изменяются.
func TestRuntimeSecretDraftReadinessComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	s, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	p := runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secret-drafts.readiness.check")
	admin := evidenceRetentionAdministrator(t, ctx, pool.Config().ConnConfig)
	ready := func(t *testing.T, wantUnavailable bool) {
		t.Helper()
		err := s.CheckRuntimeSecretDraftWork(ctx, p)
		if wantUnavailable && !errors.Is(err, errs.ErrUnavailable) || !wantUnavailable && err != nil {
			t.Fatalf("draft readiness mismatch: want_unavailable=%t admitted=%t unavailable=%t", wantUnavailable, err == nil, errors.Is(err, errs.ErrUnavailable))
		}
	}
	ready(t, false)
	for _, table := range []string{"runtime_secret_drafts", "runtime_secret_draft_operations", "runtime_secret_draft_impact_plans", "runtime_secret_draft_impact_items"} {
		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE"} {
			t.Run(table+"/"+privilege, func(t *testing.T) {
				object := "TABLE " + pgx.Identifier{"control_plane", table}.Sanitize()
				if _, err := admin.Exec(ctx, "REVOKE "+privilege+" ON "+object+" FROM control_plane_runtime"); err != nil {
					t.Fatal("revoke isolated table privilege")
				}
				defer func() {
					if _, err := admin.Exec(ctx, "GRANT "+privilege+" ON "+object+" TO control_plane_runtime"); err != nil {
						t.Error("restore isolated table privilege")
					}
				}()
				ready(t, true)
			})
			ready(t, false)
		}
	}
	for _, privilege := range []string{"USAGE", "SELECT"} {
		t.Run("generation_sequence/"+privilege, func(t *testing.T) {
			if _, err := admin.Exec(ctx, "REVOKE "+privilege+" ON SEQUENCE control_plane.runtime_secret_drafts_generation_seq FROM control_plane_runtime"); err != nil {
				t.Fatal("revoke isolated sequence privilege")
			}
			defer func() {
				if _, err := admin.Exec(ctx, "GRANT "+privilege+" ON SEQUENCE control_plane.runtime_secret_drafts_generation_seq TO control_plane_runtime"); err != nil {
					t.Error("restore isolated sequence privilege")
				}
			}()
			ready(t, true)
		})
		ready(t, false)
	}
	var indexDefinition string
	if err := admin.QueryRow(ctx, "SELECT pg_get_indexdef('control_plane.runtime_secret_draft_operation_active'::regclass)").Scan(&indexDefinition); err != nil {
		t.Fatal("read isolated operation index definition")
	}
	if _, err := admin.Exec(ctx, "DROP INDEX control_plane.runtime_secret_draft_operation_active"); err != nil {
		t.Fatal("remove isolated operation index")
	}
	ready(t, true)
	if _, err := admin.Exec(ctx, indexDefinition); err != nil {
		t.Fatal("restore isolated operation index")
	}
	ready(t, false)
	if err := r.CheckRuntimeSecretDraftWork(ctx, value.Principal{}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unauthorized readiness principal was not rejected")
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if err := r.CheckRuntimeSecretDraftWork(cancelled, p); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("cancelled readiness query was not rejected")
	}
}

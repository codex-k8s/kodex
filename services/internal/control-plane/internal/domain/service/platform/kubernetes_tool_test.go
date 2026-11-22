package platform

import (
	"context"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"testing"
)

// toolRepositoryFixture observes owner dispatch, not stored authority or native effects.
// toolRepositoryFixture наблюдает передачу владельцу, не подменяя сохранённые права и внешние эффекты.
type toolRepositoryFixture struct{ *materialRepositoryFixture }

// ListKubernetesTools records one typed owner query without fabricating discovered tools.
// ListKubernetesTools фиксирует запрос владельцу без выдуманных инструментов.
func (r *toolRepositoryFixture) ListKubernetesTools(context.Context, value.Principal, query.KubernetesToolList) (entity.KubernetesToolPage, error) {
	r.calls++
	return entity.KubernetesToolPage{}, nil
}

// GetKubernetesTool records one metadata read without exposing execution authority.
// GetKubernetesTool фиксирует чтение метаданных без выдачи полномочий исполнения.
func (r *toolRepositoryFixture) GetKubernetesTool(context.Context, value.Principal, string) (entity.KubernetesToolView, error) {
	r.calls++
	return entity.KubernetesToolView{}, nil
}

// StopKubernetesTool records only closure intent; it does not simulate remote termination.
// StopKubernetesTool фиксирует запрос закрытия, не имитируя остановку удалённого процесса.
func (r *toolRepositoryFixture) StopKubernetesTool(context.Context, value.Principal, value.Mutation, string) (entity.KubernetesToolView, error) {
	r.calls++
	return entity.KubernetesToolView{}, nil
}

// TestKubernetesToolPublicBoundary rejects worker impersonation, mismatched operation and missing OCC before owner mutation.
// TestKubernetesToolPublicBoundary отклоняет подмену worker, неверную операцию и отсутствие OCC до изменения владельца.
func TestKubernetesToolPublicBoundary(t *testing.T) {
	repository := &toolRepositoryFixture{&materialRepositoryFixture{}}
	service, err := New(repository)
	if err != nil {
		t.Fatal(err)
	}
	version := int64(2)
	mutation := value.Mutation{IdempotencyKey: "synthetic-tool-stop", ExpectedVersion: &version}
	for _, route := range []struct {
		permission string
		call       func(value.Principal) error
	}{
		{"platform.integrations.kubernetes.tool.list", func(p value.Principal) error {
			_, err := service.ListKubernetesTools(t.Context(), p, query.KubernetesToolList{RunRef: "run_synthetic", NodeRef: "nod_synthetic"})
			return err
		}},
		{"platform.integrations.kubernetes.tool.get", func(p value.Principal) error {
			_, err := service.GetKubernetesTool(t.Context(), p, "inv_synthetic")
			return err
		}},
		{"platform.integrations.kubernetes.tool.stop", func(p value.Principal) error {
			_, err := service.StopKubernetesTool(t.Context(), p, mutation, "inv_synthetic")
			return err
		}},
	} {
		p := value.Principal{ActorID: "synthetic-actor", AuthorityTenant: "synthetic-tenant", CorrelationRef: "synthetic-trace", CredentialRevision: 1, CallerWorkload: "control-api-gateway", Permission: route.permission}
		before := repository.calls
		if err := route.call(p); err != nil || repository.calls != before+1 {
			t.Fatal("valid owner route", err)
		}
		p.CallerWorkload = "integration-gateway"
		if err := route.call(p); !errors.Is(err, errs.ErrForbidden) || repository.calls != before+1 {
			t.Fatal("worker entered public owner route", err)
		}
		p.CallerWorkload = "control-api-gateway"
		p.Permission = "platform.integrations.kubernetes.input.stop"
		if err := route.call(p); !errors.Is(err, errs.ErrForbidden) || repository.calls != before+1 {
			t.Fatal("wrong operation reached owner", err)
		}
	}
	p := value.Principal{ActorID: "synthetic-actor", AuthorityTenant: "synthetic-tenant", CorrelationRef: "synthetic-trace", CredentialRevision: 1, CallerWorkload: "control-api-gateway", Permission: "platform.integrations.kubernetes.tool.stop"}
	before := repository.calls
	mutation.ExpectedVersion = nil
	if _, err := service.StopKubernetesTool(t.Context(), p, mutation, "inv_synthetic"); !errors.Is(err, errs.ErrInvalid) || repository.calls != before {
		t.Fatal("missing OCC reached owner", err)
	}
	absent, err := New(&materialRepositoryFixture{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := absent.StopKubernetesTool(t.Context(), p, mutation, "inv_synthetic"); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("missing owner was bypassed", err)
	}
}

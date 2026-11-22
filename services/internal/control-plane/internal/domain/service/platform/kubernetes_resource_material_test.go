package platform

import (
	"context"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"strings"
	"testing"
)

// materialRepositoryFixture records only actual service forwarding, not durable owner acceptance.
// materialRepositoryFixture фиксирует actual service forwarding, не durable owner acceptance.
type materialRepositoryFixture struct {
	platformrepo.Repository
	calls int
	input command.KubernetesResourceMaterialBegin
}

// ResolvePrincipal preserves the synthetic verified transport identity without self-granting.
// ResolvePrincipal сохраняет synthetic verified transport identity без self-grant.
func (*materialRepositoryFixture) ResolvePrincipal(_ context.Context, p value.Principal) (value.Principal, error) {
	return p, nil
}

// BeginKubernetesResourceMaterial records exact metadata without creating a permit or artifact.
// BeginKubernetesResourceMaterial сохраняет exact metadata без создания permit либо artifact.
func (r *materialRepositoryFixture) BeginKubernetesResourceMaterial(_ context.Context, _ value.Principal, _ value.Mutation, input command.KubernetesResourceMaterialBegin) (entity.KubernetesResourceMaterialAdmission, error) {
	r.calls++
	r.input = input
	return entity.KubernetesResourceMaterialAdmission{}, nil
}

// GetKubernetesResourceMaterial records only addressed safe metadata forwarding.
// GetKubernetesResourceMaterial фиксирует лишь addressed safe metadata forwarding.
func (r *materialRepositoryFixture) GetKubernetesResourceMaterial(context.Context, value.Principal, command.KubernetesResourceMaterialRead) (entity.KubernetesResourceMaterialView, error) {
	r.calls++
	return entity.KubernetesResourceMaterialView{}, nil
}

// CancelKubernetesResourceMaterial records closure intent without simulating a remote undo.
// CancelKubernetesResourceMaterial фиксирует closure intent без имитации remote undo.
func (r *materialRepositoryFixture) CancelKubernetesResourceMaterial(context.Context, value.Principal, value.Mutation, command.KubernetesResourceMaterialCancel) (entity.KubernetesResourceMaterialView, error) {
	r.calls++
	return entity.KubernetesResourceMaterialView{}, nil
}

// TestKubernetesResourceMaterialPublicCommandsRequireExactCaller proves public service methods
// reject wrong workload/permission before persistence and preserve the exact typed owner input.
// TestKubernetesResourceMaterialPublicCommandsRequireExactCaller доказывает отказ wrong workload/
// permission до persistence и сохранение exact typed owner input.
func TestKubernetesResourceMaterialPublicCommandsRequireExactCaller(t *testing.T) {
	repository := &materialRepositoryFixture{}
	service, err := New(repository)
	if err != nil {
		t.Fatal("construct material service fixture")
	}
	version := int64(3)
	mutation := value.Mutation{IdempotencyKey: "material-synthetic-idempotency", ExpectedVersion: &version}
	input := command.KubernetesResourceMaterialBegin{ConnectionRef: "con_synthetic", ExpectedConnectionVersion: 7, Operation: "kubernetes.create", Namespace: "example", Name: "selected", SecretType: "Opaque", ContentSHA256: strings.Repeat("a", 64), ContentBytes: 42}
	for _, route := range []struct {
		permission string
		call       func(value.Principal) error
	}{
		{"platform.integrations.kubernetes.material.begin", func(p value.Principal) error {
			_, err := service.BeginKubernetesResourceMaterial(t.Context(), p, mutation, input)
			return err
		}},
		{"platform.integrations.kubernetes.material.get", func(p value.Principal) error {
			_, err := service.GetKubernetesResourceMaterial(t.Context(), p, command.KubernetesResourceMaterialRead{ConnectionRef: input.ConnectionRef, MaterialRef: "krs_synthetic_material"})
			return err
		}},
		{"platform.integrations.kubernetes.material.cancel", func(p value.Principal) error {
			_, err := service.CancelKubernetesResourceMaterial(t.Context(), p, mutation, command.KubernetesResourceMaterialCancel{ConnectionRef: input.ConnectionRef, MaterialRef: "krs_synthetic_material"})
			return err
		}},
	} {
		p := value.Principal{ActorID: "synthetic-technical-actor", AuthorityTenant: "synthetic-tenant", CorrelationRef: "synthetic-trace", CredentialRevision: 1, CallerWorkload: "control-api-gateway", Permission: route.permission}
		before := repository.calls
		if route.call(p) != nil || repository.calls != before+1 {
			t.Fatal("exact material public command did not reach owner port")
		}
		p.CallerWorkload = "secret-broker"
		if err := route.call(p); !errors.Is(err, errs.ErrForbidden) || repository.calls != before+1 {
			t.Fatal("private workload borrowed public actor authority")
		}
		p.CallerWorkload, p.Permission = "control-api-gateway", "platform.integrations.kubernetes.setup.begin"
		if err := route.call(p); !errors.Is(err, errs.ErrForbidden) || repository.calls != before+1 {
			t.Fatal("identity setup permission became material authority")
		}
	}
	if repository.input != input {
		t.Fatal("service changed exact operation/content/destination metadata")
	}
}

// TestKubernetesResourceMaterialBeginRejectsAmbiguousDestinations keeps private material admission
// scoped to one Secret and native patch dialect; invalid metadata never reaches the owner port.
// TestKubernetesResourceMaterialBeginRejectsAmbiguousDestinations ограничивает private material
// admission одним Secret/native patch dialect; invalid metadata не достигает owner port.
func TestKubernetesResourceMaterialBeginRejectsAmbiguousDestinations(t *testing.T) {
	repository := &materialRepositoryFixture{}
	service, _ := New(repository)
	version := int64(1)
	mutation := value.Mutation{IdempotencyKey: "material-synthetic-idempotency", ExpectedVersion: &version}
	p := value.Principal{ActorID: "synthetic-technical-actor", AuthorityTenant: "synthetic-tenant", CorrelationRef: "synthetic-trace", CredentialRevision: 1, CallerWorkload: "control-api-gateway", Permission: "platform.integrations.kubernetes.material.begin"}
	base := command.KubernetesResourceMaterialBegin{ConnectionRef: "con_synthetic", ExpectedConnectionVersion: 7, Operation: "kubernetes.create", Namespace: "example", Name: "selected", SecretType: "Opaque", ContentSHA256: strings.Repeat("a", 64), ContentBytes: 42}
	for _, change := range []func(*command.KubernetesResourceMaterialBegin){
		func(i *command.KubernetesResourceMaterialBegin) { i.Namespace = "@all-namespaces" },
		func(i *command.KubernetesResourceMaterialBegin) { i.Namespace = "" },
		func(i *command.KubernetesResourceMaterialBegin) { i.Name = "" },
		func(i *command.KubernetesResourceMaterialBegin) { i.GenerateName = "also-" },
		func(i *command.KubernetesResourceMaterialBegin) { i.SecretType = "kubernetes.io/service-account-token" },
		func(i *command.KubernetesResourceMaterialBegin) { i.SecretType = "" },
		func(i *command.KubernetesResourceMaterialBegin) { i.SecretType = string([]byte{0xff}) },
		func(i *command.KubernetesResourceMaterialBegin) { i.Operation = "kubernetes.get" },
		func(i *command.KubernetesResourceMaterialBegin) { i.PatchType = "JSON" },
		func(i *command.KubernetesResourceMaterialBegin) { i.Operation = "kubernetes.patch" },
		func(i *command.KubernetesResourceMaterialBegin) { i.ContentBytes = 0 },
		func(i *command.KubernetesResourceMaterialBegin) { i.ContentBytes = 1<<20 + 1 },
		func(i *command.KubernetesResourceMaterialBegin) { i.ContentSHA256 = "" },
	} {
		input := base
		change(&input)
		if _, err := service.BeginKubernetesResourceMaterial(t.Context(), p, mutation, input); !errors.Is(err, errs.ErrInvalid) || repository.calls != 0 {
			t.Fatal("ambiguous metadata reached material persistence")
		}
	}
	for _, operation := range []string{"kubernetes.create", "kubernetes.replace", "kubernetes.apply", "kubernetes.edit", "kubernetes.patch"} {
		input := base
		input.Operation = operation
		if operation == "kubernetes.patch" {
			input.PatchType = "JSON"
		}
		if _, err := service.BeginKubernetesResourceMaterial(t.Context(), p, mutation, input); err != nil {
			t.Fatal("registered material operation was rejected")
		}
	}
}

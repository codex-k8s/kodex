package grpc

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"strings"
	"testing"
	"time"
)

// TestKubernetesResourceMaterialTransportRejectsUnknownBeforeOwner closes all public methods
// before domain interpretation when the authenticated exact-method context is absent/future.
// TestKubernetesResourceMaterialTransportRejectsUnknownBeforeOwner закрывает public methods
// до domain interpretation при отсутствии authenticated exact-method context/future fields.
func TestKubernetesResourceMaterialTransportRejectsUnknownBeforeOwner(t *testing.T) {
	server := &Server{}
	ctx := context.Background()
	for _, message := range []proto.Message{&cp.BeginManagedKubernetesResourceMaterialRequest{}, &cp.GetManagedKubernetesResourceMaterialRequest{}, &cp.CancelManagedKubernetesResourceMaterialRequest{}} {
		var err error
		switch input := message.(type) {
		case *cp.BeginManagedKubernetesResourceMaterialRequest:
			_, err = server.BeginManagedKubernetesResourceMaterial(ctx, input)
		case *cp.GetManagedKubernetesResourceMaterialRequest:
			_, err = server.GetManagedKubernetesResourceMaterial(ctx, input)
		case *cp.CancelManagedKubernetesResourceMaterialRequest:
			_, err = server.CancelManagedKubernetesResourceMaterial(ctx, input)
		}
		if status.Code(err) != codes.Unauthenticated {
			t.Fatal("material RPC acquired authority without exact-method authentication")
		}
		message.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		if managedKubernetesWireValid(message) {
			t.Fatal("future material RPC field reached semantic interpretation")
		}
	}
}

// TestKubernetesResourceMaterialProjectionPreservesSafeHistory keeps expired historical metadata
// distinct from target Secret creation and refuses unknown reasons/states/partial ownership pins.
// TestKubernetesResourceMaterialProjectionPreservesSafeHistory отделяет expired history metadata
// от target Secret creation и отклоняет unknown reasons/states/partial ownership pins.
func TestKubernetesResourceMaterialProjectionPreservesSafeHistory(t *testing.T) {
	base := entity.KubernetesResourceMaterialView{Ref: "krs_synthetic_material", ConnectionRef: "int_synthetic", IdentityRef: "kbi_synthetic", TargetRef: "kbt_synthetic", Namespace: "example", Name: "selected", Operation: "kubernetes.create", SecretType: "Opaque", Version: 3, ConnectionVersion: 7, IdentityRevision: 2, BindingVersion: 3, Policy: entity.IntegrationCredentialInitiatorRequired, State: enum.KubernetesResourceMaterialAvailable, ExpiresAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	view, err := kubernetesResourceMaterialToProto(base)
	if err != nil || view.GetState() != cp.ManagedKubernetesResourceMaterialState_MANAGED_KUBERNETES_RESOURCE_MATERIAL_STATE_AVAILABLE || view.GetExpiresAt().AsTime() != base.ExpiresAt || view.GetIdentityRevision() != 2 {
		t.Fatal("safe material history was promoted into fresh authority or lost pins")
	}
	for _, change := range []func(*entity.KubernetesResourceMaterialView){
		func(v *entity.KubernetesResourceMaterialView) { v.State = "FUTURE" },
		func(v *entity.KubernetesResourceMaterialView) { v.ReasonCode = "private provider diagnostic" },
		func(v *entity.KubernetesResourceMaterialView) { v.SecretType = "kubernetes.io/service-account-token" },
		func(v *entity.KubernetesResourceMaterialView) { v.Namespace = "@cluster" },
		func(v *entity.KubernetesResourceMaterialView) { v.GenerateName = "also-" },
		func(v *entity.KubernetesResourceMaterialView) { v.IdentityRevision = 0 },
		func(v *entity.KubernetesResourceMaterialView) { v.BindingVersion = 0 },
		func(v *entity.KubernetesResourceMaterialView) { v.Policy = entity.IntegrationCredentialProjectShared },
		func(v *entity.KubernetesResourceMaterialView) { v.Version = 9007199254740992 },
	} {
		input := base
		change(&input)
		if out, err := kubernetesResourceMaterialToProto(input); err == nil || out != nil {
			t.Fatal("malformed owner metadata escaped material public projection")
		}
	}
	base.ReasonCode = "MATERIAL_OBSERVATION_ABSENT"
	view, err = kubernetesResourceMaterialToProto(base)
	if err != nil || !strings.Contains(view.GetReasonCode(), "OBSERVATION_ABSENT") || view.GetState() != cp.ManagedKubernetesResourceMaterialState_MANAGED_KUBERNETES_RESOURCE_MATERIAL_STATE_AVAILABLE {
		t.Fatal("reason code silently changed artifact state")
	}
}

package platform

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestPersonalInvocationDispatchDeadlinePreservesSpecializedLanes fixes the composition boundary: only ordinary personal TOKEN gets the finite envelope; typed broker and EMAIL/NONE owners retain their own admission and credit lifecycles.
// TestPersonalInvocationDispatchDeadlinePreservesSpecializedLanes закрепляет composition boundary: лишь ordinary personal TOKEN получает finite envelope; typed broker и EMAIL/NONE owners сохраняют собственный admission/credit lifecycle.
func TestPersonalInvocationDispatchDeadlinePreservesSpecializedLanes(t *testing.T) {
	lease := time.Now().Add(30 * time.Second)
	until := lease.Add(-20 * time.Second)
	for _, scenario := range []struct {
		name   string
		policy entity.IntegrationCredentialPolicy
		kind   integrationpackage.CredentialDeliveryKind
		want   time.Time
	}{
		{"ordinary personal token", entity.IntegrationCredentialInitiatorRequired, integrationpackage.CredentialDeliveryToken, until},
		{"shared token", entity.IntegrationCredentialProjectShared, integrationpackage.CredentialDeliveryToken, lease},
		{"personal Kubernetes SSH broker", entity.IntegrationCredentialInitiatorRequired, integrationpackage.CredentialDeliveryBrokerIdentity, lease},
		{"shared Kubernetes SSH broker", entity.IntegrationCredentialProjectShared, integrationpackage.CredentialDeliveryBrokerIdentity, lease},
		{"personal email none", entity.IntegrationCredentialInitiatorRequired, integrationpackage.CredentialDeliveryNone, lease},
		{"shared email none", entity.IntegrationCredentialProjectShared, integrationpackage.CredentialDeliveryNone, lease},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if got := personalInvocationDispatchUntil(scenario.policy, scenario.kind, lease, until); !got.Equal(scenario.want) {
				t.Fatal("ordinary dispatch guard changed a specialized owner lane")
			}
		})
	}
	for _, allowance := range []time.Time{{}, lease.Add(time.Hour)} {
		if got := personalInvocationDispatchUntil(entity.IntegrationCredentialInitiatorRequired, integrationpackage.CredentialDeliveryToken, lease, allowance); !got.Equal(lease) {
			t.Fatal("allowance extended reconciliation lease")
		}
	}
}

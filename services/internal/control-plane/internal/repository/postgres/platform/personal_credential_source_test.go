package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestPersonalCredentialRowKeepsStoredSourceOneof preserves explicit TOKEN metadata and
// broker-only safe metadata while rejecting mixed, absent or unknown stored discriminators.
// Historical receipt decoding is intentionally separate and does not use this fresh row decoder.
// TestPersonalCredentialRowKeepsStoredSourceOneof сохраняет явные TOKEN metadata и broker-only
// safe metadata, отвергая mixed, absent либо unknown stored discriminators.
// Сохранённые receipts разбираются отдельно без этого fresh row decoder.
func TestPersonalCredentialRowKeepsStoredSourceOneof(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	token := personalCredentialRow{value: entity.PersonalIntegrationCredential{Ref: "pic_synthetic_binding", Kind: entity.IntegrationCredentialToken, State: "CHECK_REQUIRED"},
		revision: entity.IntegrationCredentialRevision{Ref: "pcr_synthetic_revision", Revision: 1, SecretRef: "kodex-system/kodex-integration-credentials#synthetic-key",
			SecretUID: "cc4f68c8-80f6-4591-87fe-cc610b541d99", SecretResourceVersion: "42", ContentSHA256: strings.Repeat("a", 64)}, createdAt: &created}
	if binding, err := token.decode(); err != nil || binding.Kind != entity.IntegrationCredentialToken || binding.Credential == nil || binding.BrokerIdentity != nil {
		t.Fatal("explicit stored TOKEN metadata changed representation")
	}
	broker := personalCredentialRow{value: entity.PersonalIntegrationCredential{Ref: "pic_synthetic_binding", Kind: entity.IntegrationCredentialBrokerIdentity,
		BrokerPurpose: entity.IntegrationBrokerIdentityKubernetes, State: "CHECK_REQUIRED"},
		brokerRevision: entity.IntegrationBrokerIdentityRevision{Ref: "kbi_synthetic_revision", TargetRef: "kbt_synthetic_target", Revision: 2,
			Purpose: entity.IntegrationBrokerIdentityKubernetes}, brokerCreatedAt: &created}
	binding, err := broker.decode()
	if err != nil || binding.Credential != nil || binding.BrokerIdentity == nil || binding.BrokerIdentity.Ref != broker.brokerRevision.Ref ||
		binding.BrokerIdentity.TargetRef != broker.brokerRevision.TargetRef || binding.BrokerIdentity.Revision != 2 || !binding.BrokerIdentity.CreatedAt.Equal(created) {
		t.Fatal("stored broker identity lost its safe metadata or acquired a token descriptor")
	}
	for _, mutate := range []func(*personalCredentialRow){
		func(r *personalCredentialRow) { r.value.Kind = "" },
		func(r *personalCredentialRow) { r.value.Kind = "FUTURE" },
		func(r *personalCredentialRow) { r.value.Kind = entity.IntegrationCredentialToken },
		func(r *personalCredentialRow) { r.value.BrokerPurpose = entity.IntegrationBrokerIdentitySSH },
		func(r *personalCredentialRow) { r.brokerRevision.Purpose = entity.IntegrationBrokerIdentitySSH },
		func(r *personalCredentialRow) { r.brokerRevision.TargetRef = "" },
		func(r *personalCredentialRow) { r.brokerRevision.Revision = 9007199254740992 },
		func(r *personalCredentialRow) { r.brokerCreatedAt = nil },
		func(r *personalCredentialRow) { r.brokerRevision.Ref = "" },
		func(r *personalCredentialRow) { r.revision, r.createdAt = token.revision, token.createdAt },
		func(r *personalCredentialRow) { r.value.VerifiedCredentialRef = token.revision.Ref },
		func(r *personalCredentialRow) { r.value.State = "READY" },
		func(r *personalCredentialRow) { r.value.VerifiedBrokerIdentityRef = r.brokerRevision.Ref },
	} {
		changed := broker
		mutate(&changed)
		if _, err := changed.decode(); err == nil {
			t.Fatal("mixed, partial or stale stored identity acquired a fresh source")
		}
	}
	for _, mutate := range []func(*personalCredentialRow){
		func(r *personalCredentialRow) { r.value.Kind = "" },
		func(r *personalCredentialRow) { r.value.BrokerPurpose = entity.IntegrationBrokerIdentityKubernetes },
		func(r *personalCredentialRow) { r.value.VerifiedBrokerIdentityRef = "kbi_synthetic_revision" },
	} {
		changed := token
		mutate(&changed)
		if _, err := changed.decode(); err == nil {
			t.Fatal("stored TOKEN row inferred or borrowed a different source")
		}
	}
	ready := broker
	ready.value.State, ready.value.VerifiedBrokerIdentityRef = "READY", broker.brokerRevision.Ref
	if _, err := ready.decode(); err != nil {
		t.Fatal("exact verified broker identity was rejected")
	}
	missing := personalCredentialRow{value: entity.PersonalIntegrationCredential{ConnectionRef: "con_synthetic"}}
	if binding, err := missing.decode(); err != nil || binding.Kind != "" || binding.Credential != nil || binding.BrokerIdentity != nil {
		t.Fatal("absent catalog binding acquired a stored source")
	}
}

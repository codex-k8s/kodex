package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// personalCredentialTransportFixture supplies private synthetic descriptor metadata that must
// never appear in a public RPC response, without holding any real provider credential.
// personalCredentialTransportFixture задаёт закрытые synthetic descriptor metadata, которые не
// должны появляться в публичном RPC response, без настоящей provider credential.
func personalCredentialTransportFixture(now time.Time) entity.PersonalIntegrationCredential {
	return entity.PersonalIntegrationCredential{Ref: "ipc_fixture01", OrganizationID: "org_private_fixture", SubjectID: "usr_private_fixture",
		ConnectionRef: "int_fixture01", ConnectionVersion: 4, Version: 2, Generation: 2,
		CredentialPolicy: entity.IntegrationCredentialInitiatorRequired, State: "CHECK_REQUIRED",
		Credential: &entity.IntegrationCredentialRevision{Ref: "ipcr_private_fixture", Revision: 1, CreatedAt: now,
			SecretRef: "kodex-system/kodex-integration-credentials#integration-private-fixture", SecretUID: "10000000-0000-4000-8000-000000000001",
			SecretResourceVersion: "private-resource-version", ContentSHA256: strings.Repeat("a", 64)}}
}

// TestPersonalCredentialRPCProjectionExcludesStorageAndOwner checks the actual protobuf JSON
// output and derived expiry, while rejecting unknown policy/state and malformed storage metadata.
// TestPersonalCredentialRPCProjectionExcludesStorageAndOwner проверяет реальный protobuf JSON
// output и производный срок, отклоняя unknown policy/state и неверные storage metadata.
func TestPersonalCredentialRPCProjectionExcludesStorageAndOwner(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	binding := personalCredentialTransportFixture(now)
	view, err := castPersonalIntegrationCredential(binding, now)
	if err != nil {
		t.Fatal("safe personal projection failed")
	}
	raw, err := protojson.Marshal(view)
	if err != nil {
		t.Fatal("safe personal response encoding failed")
	}
	for _, private := range []string{binding.OrganizationID, binding.SubjectID, binding.Credential.Ref, binding.Credential.SecretRef,
		binding.Credential.SecretUID, binding.Credential.SecretResourceVersion, binding.Credential.ContentSHA256} {
		if strings.Contains(string(raw), private) {
			t.Fatal("personal response exposed private descriptor metadata")
		}
	}
	binding.State = "READY"
	binding.ExpiresAt = &now
	view, err = castPersonalIntegrationCredential(binding, now)
	if err != nil || view.GetState() != cp.PersonalIntegrationCredentialState_PERSONAL_INTEGRATION_CREDENTIAL_STATE_EXPIRED || binding.State != "READY" {
		t.Fatal("expired view rewrote or misclassified persistent state")
	}
	for _, mutate := range []func(*entity.PersonalIntegrationCredential){
		func(b *entity.PersonalIntegrationCredential) { b.State = "UNKNOWN" },
		func(b *entity.PersonalIntegrationCredential) { b.State = "EXPIRED" },
		func(b *entity.PersonalIntegrationCredential) { b.CredentialPolicy = "UNKNOWN" },
		func(b *entity.PersonalIntegrationCredential) { b.Version = 0 },
		func(b *entity.PersonalIntegrationCredential) { b.ConnectionVersion = 0 },
		func(b *entity.PersonalIntegrationCredential) { b.Credential = nil },
	} {
		invalid := personalCredentialTransportFixture(now)
		mutate(&invalid)
		if _, err := castPersonalIntegrationCredential(invalid, now); status.Code(err) != codes.Unavailable {
			t.Fatal("invalid personal metadata was projected")
		}
	}
}

// TestPersonalCredentialUnauthenticatedRPCDoesNotRetainValue verifies denial occurs before a
// domain call and clears the received byte buffer without reflecting it in diagnostics.
// TestPersonalCredentialUnauthenticatedRPCDoesNotRetainValue проверяет отказ до domain call и
// очистку принятого byte buffer без отражения значения в диагностике.
func TestPersonalCredentialUnauthenticatedRPCDoesNotRetainValue(t *testing.T) {
	t.Parallel()
	value := []byte("synthetic-private-input")
	request := &cp.ConfigurePersonalIntegrationCredentialRequest{ConnectionRef: "int_fixture01", CredentialValue: value}
	_, err := (&Server{}).ConfigurePersonalIntegrationCredential(context.Background(), request)
	if status.Code(err) != codes.Unauthenticated || request.CredentialValue != nil || strings.Contains(err.Error(), "synthetic-private-input") {
		t.Fatal("unauthenticated personal write retained or exposed input")
	}
	for _, octet := range value {
		if octet != 0 {
			t.Fatal("received credential buffer was not cleared")
		}
	}
}

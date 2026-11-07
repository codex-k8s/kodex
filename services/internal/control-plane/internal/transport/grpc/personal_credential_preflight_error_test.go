package grpc

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestPersonalPreflightTransportKeepsFullClosedList checks the normal error mapper preserves all
// safe issues in one typed detail and rejects malformed/duplicate owner output without raw data.
// TestPersonalPreflightTransportKeepsFullClosedList проверяет сохранение всех safe issues обычным
// error mapper в одном typed detail и отказ malformed/duplicate owner output без raw data.
func TestPersonalPreflightTransportKeepsFullClosedList(t *testing.T) {
	issues := []entity.IntegrationCredentialPreflightIssue{{ConnectionRef: "int_fixture01", CapabilityKey: "gitlab.project.metadata.read", Problem: entity.IntegrationCredentialRevoked}, {ConnectionRef: "int_fixture02", CapabilityKey: "jira.issue.read", Problem: entity.IntegrationCredentialMissing}}
	err := transportError(&platformservice.IntegrationCredentialPreflightError{Issues: issues})
	value := status.Convert(err)
	if value.Code() != codes.FailedPrecondition || len(value.Details()) != 1 {
		t.Fatal("preflight lost exact status or detail count")
	}
	detail, ok := value.Details()[0].(*cp.IntegrationCredentialPreflightDetail)
	if !ok || len(detail.Issues) != 2 || detail.Issues[0].Problem != cp.IntegrationCredentialPreflightProblem_INTEGRATION_CREDENTIAL_PREFLIGHT_PROBLEM_CREDENTIAL_REVOKED || detail.Issues[1].Problem != cp.IntegrationCredentialPreflightProblem_INTEGRATION_CREDENTIAL_PREFLIGHT_PROBLEM_CREDENTIAL_MISSING {
		t.Fatal("preflight lost complete typed problems")
	}
	for _, invalid := range [][]entity.IntegrationCredentialPreflightIssue{nil, {issues[0], issues[0]}, {{ConnectionRef: "https://other.example.invalid", CapabilityKey: "read", Problem: entity.IntegrationCredentialMissing}}, {{ConnectionRef: "int_fixture01", CapabilityKey: "read", Problem: "UNKNOWN"}}} {
		value := status.Convert(transportError(&platformservice.IntegrationCredentialPreflightError{Issues: invalid}))
		if value.Code() != codes.Internal || len(value.Details()) != 0 {
			t.Fatal("invalid owner preflight produced public diagnostics")
		}
	}
}

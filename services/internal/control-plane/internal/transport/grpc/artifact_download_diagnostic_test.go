package grpc

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/observability/artifactread"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestArtifactDownloadDiagnosticPreservesExactGRPCStatus(t *testing.T) {
	for _, outcome := range []error{errs.ErrUnavailable, errs.ErrConflict} {
		err := artifactread.WithFailure(outcome, artifactread.NewDiagnostic(artifactread.StageAudit, artifactread.ClassPostgres, "40001"))
		err = artifactread.WithAttempt(err, 3, true)
		mapped := artifactDownloadTransportError(err)
		if !proto.Equal(status.Convert(mapped).Proto(), status.Convert(transportError(outcome)).Proto()) || mapped.Error() != transportError(outcome).Error() || !errors.Is(mapped, outcome) {
			t.Fatal("GRPCStatus text/code/details or domain sentinel changed")
		}
		diagnostic, ok := artifactread.From(mapped)
		if !ok || diagnostic.Stage != artifactread.StageAudit || diagnostic.SQLState != "40001" || diagnostic.Attempt != 3 || !diagnostic.RetryExhausted {
			t.Fatal("safe diagnostic did not reach server error observer")
		}
	}
	for _, outcome := range []error{errs.ErrForbidden, errs.ErrNotFound, errs.ErrInvalid} {
		actual := artifactDownloadTransportError(outcome)
		if !proto.Equal(status.Convert(actual).Proto(), status.Convert(transportError(outcome)).Proto()) {
			t.Fatal("expected denial changed")
		}
		if _, ok := artifactread.From(actual); ok {
			t.Fatal("expected denial received invented diagnostic")
		}
	}
}

func TestArtifactDownloadDiagnosticTransportDropsJoinedRawCause(t *testing.T) {
	safe := artifactread.WithFailure(errs.ErrUnavailable, artifactread.NewDiagnostic(artifactread.StageAudit, artifactread.ClassPostgres, "PRIVATE_SQLSTATE"))
	raw := errors.New("PRIVATE_TOKEN PRIVATE_QUERY user@example.test")
	actual := artifactDownloadTransportError(errors.Join(safe, raw))
	if errors.Is(actual, raw) || strings.Contains(actual.Error(), "PRIVATE") || !errors.Is(actual, errs.ErrUnavailable) {
		t.Fatal("transport retained raw diagnostic or changed domain mapping")
	}
	diagnostic, ok := artifactread.From(actual)
	if !ok || diagnostic.SQLState != "OTHER" {
		t.Fatal("unknown SQLSTATE was not closed")
	}
}

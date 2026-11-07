package grpc

import (
	"regexp"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// personalPreflightReference bounds owner-issued opaque refs before attaching public diagnostics.
// personalPreflightReference ограничивает owner-issued opaque refs до публичной диагностики.
var personalPreflightReference = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// personalPreflightCapability excludes arbitrary strings and control characters from safe metadata.
// personalPreflightCapability исключает произвольные строки и control characters из safe metadata.
var personalPreflightCapability = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,256}$`)

// personalCredentialPreflightError encodes exactly one bounded typed problem list and never raw
// provider text or Secret data. Invalid owner output fails closed rather than becoming a partial list.
// personalCredentialPreflightError кодирует ровно один bounded typed problem list без provider text
// и Secret data. Invalid owner output закрыто отклоняется, не становясь частичным списком.
func personalCredentialPreflightError(failure *platformservice.IntegrationCredentialPreflightError) error {
	invalid := status.Error(codes.Internal, "personal credential preflight result is invalid")
	if failure == nil || len(failure.Issues) < 1 || len(failure.Issues) > 256 {
		return invalid
	}
	detail := &cp.IntegrationCredentialPreflightDetail{}
	seen := make(map[[2]string]bool, len(failure.Issues))
	for _, issue := range failure.Issues {
		key := [2]string{issue.ConnectionRef, issue.CapabilityKey}
		problem, known := cp.IntegrationCredentialPreflightProblem_value["INTEGRATION_CREDENTIAL_PREFLIGHT_PROBLEM_"+string(issue.Problem)]
		if !known || problem == 0 || seen[key] || !personalPreflightReference.MatchString(issue.ConnectionRef) || !personalPreflightCapability.MatchString(issue.CapabilityKey) {
			return invalid
		}
		seen[key] = true
		name := issue.ConnectionName
		if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 200 || strings.ContainsAny(name, "\r\n\x00") {
			name = ""
		}
		detail.Issues = append(detail.Issues, &cp.IntegrationCredentialPreflightIssue{ConnectionRef: issue.ConnectionRef, CapabilityKey: issue.CapabilityKey, Problem: cp.IntegrationCredentialPreflightProblem(problem), ConnectionName: name})
	}
	result, err := status.New(codes.FailedPrecondition, "required personal integration credentials are not ready").WithDetails(detail)
	if err != nil {
		return invalid
	}
	return result.Err()
}

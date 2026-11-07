package httptransport

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// personalCredentialPreflightRequired identifies a nonretryable launch precondition, not a tool grant.
// personalCredentialPreflightRequired обозначает nonretryable launch precondition, не tool grant.
const personalCredentialPreflightRequired = "INTEGRATION_CREDENTIALS_REQUIRED"

// personalPreflightReference accepts only bounded opaque owner references in diagnostic metadata.
// personalPreflightReference принимает только bounded opaque owner refs в диагностике.
var personalPreflightReference = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// personalPreflightCapability confines diagnostic keys to the public capability alphabet and bound.
// personalPreflightCapability ограничивает diagnostic keys публичным алфавитом и пределом capability.
var personalPreflightCapability = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,256}$`)

// writePersonalCredentialPreflightProblem handles only the exact typed owner diagnostic. Wrong
// status, duplicate/unknown/partial metadata or extra details produce a safe502 without raw payloads.
// writePersonalCredentialPreflightProblem обрабатывает только exact typed owner diagnostic. Wrong
// status, duplicate/unknown/partial metadata и extra details дают safe502 без raw payloads.
func writePersonalCredentialPreflightProblem(writer http.ResponseWriter, err error) bool {
	upstream := status.Convert(err)
	details := upstream.Details()
	var detail *cp.IntegrationCredentialPreflightDetail
	for _, item := range details {
		if candidate, ok := item.(*cp.IntegrationCredentialPreflightDetail); ok {
			detail = candidate
			break
		}
	}
	if detail == nil {
		return false
	}
	invalid := func() bool {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return true
	}
	if upstream.Code() != codes.FailedPrecondition || len(details) != 1 || len(detail.ProtoReflect().GetUnknown()) != 0 || len(detail.Issues) < 1 || len(detail.Issues) > 256 {
		return invalid()
	}
	issues := make([]map[string]string, 0, len(detail.Issues))
	seen := make(map[[2]string]bool, len(detail.Issues))
	for _, item := range detail.Issues {
		if item == nil || len(item.ProtoReflect().GetUnknown()) != 0 || item.Problem == 0 || !personalPreflightReference.MatchString(item.ConnectionRef) || !personalPreflightCapability.MatchString(item.CapabilityKey) {
			return invalid()
		}
		name, known := cp.IntegrationCredentialPreflightProblem_name[int32(item.Problem)]
		key := [2]string{item.ConnectionRef, item.CapabilityKey}
		if !known || seen[key] || !utf8.ValidString(item.ConnectionName) || utf8.RuneCountInString(item.ConnectionName) > 200 || strings.ContainsAny(item.ConnectionName, "\r\n\x00") {
			return invalid()
		}
		seen[key] = true
		view := map[string]string{"connectionRef": item.ConnectionRef, "capabilityKey": item.CapabilityKey, "problem": strings.TrimPrefix(name, "INTEGRATION_CREDENTIAL_PREFLIGHT_PROBLEM_")}
		if item.ConnectionName != "" {
			view["connectionName"] = item.ConnectionName
		}
		issues = append(issues, view)
	}
	value := map[string]any{"type": "urn:kodex:problem:integration_credentials_required", "title": "Personal integration credentials are required before launch", "status": http.StatusConflict, "code": personalCredentialPreflightRequired, "correlationId": uuid.NewString(), "retryable": false, "integrationCredentialIssues": issues}
	if localizer, ok := writer.(interface{ Localize(string) string }); ok {
		value["title"] = localizer.Localize(personalCredentialPreflightRequired)
	}
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(writer).Encode(value)
	return true
}

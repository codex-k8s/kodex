package httptransport

import (
	"net/http"
	"regexp"
	"strconv"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

var opaqueHTTPReference = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
var httpIdempotencyKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func validHTTPPage(size *int, token *string) bool {
	return (size == nil || *size >= 1 && *size <= 100) && (token == nil || len(*token) <= 512)
}

func requireVersionedMutation(w http.ResponseWriter, key, etag string) (*controlplanev1.MutationContext, bool) {
	result, ok := mutation(key, etag)
	if !ok || !httpIdempotencyKey.MatchString(key) || result.GetExpectedVersion() < 1 || result.GetExpectedVersion() > maximumSafeJSONInteger ||
		etag != "\""+strconv.FormatInt(result.GetExpectedVersion(), 10)+"\"" {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return result, true
}

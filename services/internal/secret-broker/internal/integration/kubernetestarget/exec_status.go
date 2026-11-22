package kubernetestarget

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/remotecommand"
	strictjson "sigs.k8s.io/json"
)

const (
	// maximumExecStatusBytes bounds the private remotecommand control channel independently of stdout.
	// maximumExecStatusBytes ограничивает private remotecommand control channel независимо от stdout.
	maximumExecStatusBytes = 64 << 10
)

// errExecStatusUnknown rejects missing, ambiguous or non-exit metadata without exposing remote text.
// errExecStatusUnknown отклоняет missing, ambiguous либо non-exit metadata без раскрытия remote text.
var errExecStatusUnknown = errors.New("managed Kubernetes remote execution status is unknown")

// decodeNativeExecStatus accepts only one bounded explicit v4/v5 exit receipt.
// Empty EOF is not success; duplicate fields, conflicting status/code and unexpected causes stay
// UNKNOWN. The untrusted message is never returned, logged or converted to a public diagnostic.
// decodeNativeExecStatus принимает только bounded explicit v4/v5 exit receipt.
// Empty EOF не означает success; duplicate fields, conflicting status/code и unexpected causes
// остаются UNKNOWN. Untrusted message не возвращается, не логируется и не становится public diagnostic.
func decodeNativeExecStatus(raw []byte) (int32, error) {
	if len(raw) == 0 || len(raw) > maximumExecStatusBytes || !utf8.Valid(raw) {
		return 0, errExecStatusUnknown
	}
	var status metav1.Status
	strictErrors, err := strictjson.UnmarshalStrict(raw, &status, strictjson.DisallowDuplicateFields, strictjson.DisallowUnknownFields)
	if err != nil || len(strictErrors) != 0 || status.Code != 0 ||
		(status.Kind != "" || status.APIVersion != "") && (status.Kind != "Status" || status.APIVersion != "v1") ||
		status.ResourceVersion != "" || status.Continue != "" || status.SelfLink != "" || status.RemainingItemCount != nil {
		return 0, errExecStatusUnknown
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return 0, errExecStatusUnknown
	}
	for _, field := range fields {
		if bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
			return 0, errExecStatusUnknown
		}
	}
	if status.Status == metav1.StatusSuccess && status.Reason == "" && status.Details == nil {
		return 0, nil
	}
	if status.Status != metav1.StatusFailure || status.Reason != remotecommand.NonZeroExitCodeReason || status.Details == nil ||
		status.Details.Name != "" || status.Details.Group != "" || status.Details.Kind != "" || status.Details.UID != "" ||
		status.Details.RetryAfterSeconds != 0 || len(status.Details.Causes) != 1 {
		return 0, errExecStatusUnknown
	}
	cause := status.Details.Causes[0]
	code, err := strconv.ParseUint(cause.Message, 10, 8)
	if err != nil || code == 0 || strconv.FormatUint(code, 10) != cause.Message || cause.Type != remotecommand.ExitCodeCauseType || cause.Field != "" {
		return 0, errExecStatusUnknown
	}
	return int32(code), nil
}

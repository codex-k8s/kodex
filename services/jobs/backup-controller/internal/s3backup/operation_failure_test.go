package s3backup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"

	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// TestOperationFailureRedactsNativeDetails proves typed SDK classes survive without retaining native error chains or sensitive strings.
// RU: TestOperationFailureRedactsNativeDetails доказывает сохранение классов SDK без исходной цепочки ошибок и чувствительных строк.
func TestOperationFailureRedactsNativeDetails(t *testing.T) {
	t.Parallel()
	const privateMarker = "PRIVATE_FIXTURE_URL_HEADER_MESSAGE"
	cases := []struct {
		name  string
		cause error
		class string
	}{
		{"denied", responseError(http.StatusForbidden), "access_denied"},
		{"unauthorized", responseError(http.StatusUnauthorized), "access_denied"},
		{"throttled", responseError(http.StatusTooManyRequests), "throttled"},
		{"server", responseError(http.StatusServiceUnavailable), "server"},
		{"other_http", responseError(http.StatusBadRequest), "http_other"},
		{"deadline", context.DeadlineExceeded, "deadline"},
		{"canceled", context.Canceled, "canceled"},
		{"network_timeout", &net.DNSError{Err: privateMarker, IsTimeout: true}, "network_timeout"},
		{"network", &net.DNSError{Err: privateMarker}, "network"},
		{"unknown", errors.New(privateMarker), "unknown"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			native := fmt.Errorf("%s: %w", privateMarker, test.cause)
			mapped := mapError(native)
			if mapped.Error() != "S3 operation failed: "+test.class {
				t.Fatalf("unexpected closed diagnostic: %s", mapped)
			}
			var response *smithyhttp.ResponseError
			var network net.Error
			if strings.Contains(fmt.Sprintf("%+v %#v", mapped, mapped), privateMarker) ||
				errors.Is(mapped, native) || errors.Is(mapped, test.cause) || errors.Unwrap(mapped) != nil ||
				errors.As(mapped, &response) || errors.As(mapped, &network) {
				t.Fatal("mapped failure retained native details")
			}
		})
	}
}

// TestOperationFailurePreservesRepositorySentinels keeps existing conditional-write and absence handling unchanged.
// RU: TestOperationFailurePreservesRepositorySentinels сохраняет прежнюю обработку условной записи и отсутствия объекта.
func TestOperationFailurePreservesRepositorySentinels(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusConflict, http.StatusPreconditionFailed, http.StatusNotFound} {
		expected := ErrConflict
		if status == http.StatusNotFound {
			expected = ErrNotFound
		}
		if got := mapError(fmt.Errorf("wrapped: %w", responseError(status))); got != expected {
			t.Fatalf("status %d changed sentinel identity", status)
		}
	}
}

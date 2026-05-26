package httptransport

import (
	stdhttp "net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func providerHubError(err error) *SafeError {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.FailedPrecondition:
		return WrapSafeError(stdhttp.StatusBadRequest, CodeInvalidRequest, "provider webhook is not accepted", false, err)
	case codes.ResourceExhausted:
		return WrapSafeError(stdhttp.StatusTooManyRequests, CodeRateLimited, "provider-hub rate limit is active", true, err)
	case codes.DeadlineExceeded, codes.Unavailable:
		return WrapSafeError(stdhttp.StatusServiceUnavailable, CodeDownstreamUnavailable, "provider-hub is unavailable", true, err)
	default:
		return WrapSafeError(stdhttp.StatusServiceUnavailable, CodeDownstreamUnavailable, "provider-hub is unavailable", true, err)
	}
}

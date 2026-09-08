package httptransport

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http"
)

const providerDeviceAuthorizationUnavailable = "PROVIDER_DEVICE_AUTHORIZATION_UNAVAILABLE"

func writeProviderDeviceProblem(writer http.ResponseWriter, err error) {
	if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
		writer.Header().Set("Retry-After", "1")
		writeLocalProblem(writer, http.StatusServiceUnavailable, providerDeviceAuthorizationUnavailable, true)
		return
	}
	writeRPCProblem(writer, err)
}

package httptransport

import (
	"net/http"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/sttapi/errorprofile"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func writeSpeechProblem(writer http.ResponseWriter, err error) {
	upstream := status.Convert(err)
	typed := false
	var retry *errdetails.RetryInfo
	retryCount := 0
	for _, detail := range upstream.Details() {
		switch value := detail.(type) {
		case *errdetails.ErrorInfo:
			typed = typed || value.GetDomain() == errorprofile.Domain && value.GetReason() == errorprofile.TranscriptionRateLimited
		case *errdetails.RetryInfo:
			retry, retryCount = value, retryCount+1
		}
	}
	if upstream.Code() != codes.ResourceExhausted || !typed {
		writeRPCProblem(writer, err)
		return
	}
	writer.Header().Del("Retry-After")
	retryable := false
	if retryCount == 1 && retry.GetRetryDelay() != nil && retry.GetRetryDelay().CheckValid() == nil {
		delay := retry.GetRetryDelay()
		if delay.GetNanos() == 0 && delay.GetSeconds() >= 1 && delay.GetSeconds() <= int64(errorprofile.MaximumRetryAfter/time.Second) {
			writer.Header().Set("Retry-After", strconv.FormatInt(delay.GetSeconds(), 10))
			retryable = true
		}
	}
	// Подсказка разрешает явный повтор пользователя, но не повторяет платный POST.
	writeLocalProblem(writer, http.StatusTooManyRequests, errorprofile.TranscriptionRateLimited, retryable)
}

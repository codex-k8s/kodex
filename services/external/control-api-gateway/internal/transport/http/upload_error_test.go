package httptransport

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestWriteArtifactUploadErrorPreservesFailureOrder checks nil is a true no-write path and compares wrapped/multiple sentinel and ordinary RPC failures against the previous exact branch. It pins public status/code and precedence without changing random correlation IDs or treating an attempted response as an acknowledged stream abort.
// TestWriteArtifactUploadErrorPreservesFailureOrder проверяет nil без записи и сравнивает обёрнутые и составные sentinel и обычные RPC-ошибки с прежней точной ветвью. Закрепляет публичные status/code и порядок без изменения случайных correlation IDs и без принятия попытки ответа за подтверждённую отмену потока.
func TestWriteArtifactUploadErrorPreservesFailureOrder(t *testing.T) {
	t.Parallel()
	untouched := httptest.NewRecorder()
	untouched.Code = 0
	if writeArtifactUploadError(nil, nil) || writeArtifactUploadError(untouched, nil) || untouched.Code != 0 || untouched.Body.Len() != 0 || len(untouched.Header()) != 0 || untouched.Flushed {
		t.Fatal("nil upload error performed a write")
	}
	for _, test := range []struct {
		name string
		err  error
		code string
		http int
	}{
		{name: "length", err: errArtifactContentLengthMismatch, code: "CONTENT_LENGTH_MISMATCH", http: http.StatusBadRequest},
		{name: "wrapped length", err: fmt.Errorf("private wrapper: %w", errArtifactContentLengthMismatch), code: "CONTENT_LENGTH_MISMATCH", http: http.StatusBadRequest},
		{name: "read", err: errArtifactBodyRead, code: "REQUEST_BODY_READ_FAILED", http: http.StatusBadRequest},
		{name: "wrapped read", err: fmt.Errorf("private wrapper: %w", errArtifactBodyRead), code: "REQUEST_BODY_READ_FAILED", http: http.StatusBadRequest},
		{name: "length before read", err: errors.Join(errArtifactBodyRead, errArtifactContentLengthMismatch), code: "CONTENT_LENGTH_MISMATCH", http: http.StatusBadRequest},
		{name: "length before rpc", err: errors.Join(status.Error(codes.Unavailable, "private diagnostic"), errArtifactContentLengthMismatch), code: "CONTENT_LENGTH_MISMATCH", http: http.StatusBadRequest},
		{name: "read before rpc", err: errors.Join(status.Error(codes.PermissionDenied, "private diagnostic"), errArtifactBodyRead), code: "REQUEST_BODY_READ_FAILED", http: http.StatusBadRequest},
		{name: "rpc unavailable", err: status.Error(codes.Unavailable, "private diagnostic"), code: "UNAVAILABLE", http: http.StatusServiceUnavailable},
		{name: "rpc denied", err: status.Error(codes.PermissionDenied, "private diagnostic"), code: "PERMISSION_DENIED", http: http.StatusForbidden},
		{name: "ordinary error", err: errors.New("private diagnostic"), code: "INTERNAL", http: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
			if errors.Is(test.err, errArtifactContentLengthMismatch) {
				writeLocalProblem(expected, http.StatusBadRequest, "CONTENT_LENGTH_MISMATCH", false)
			} else if errors.Is(test.err, errArtifactBodyRead) {
				writeLocalProblem(expected, http.StatusBadRequest, "REQUEST_BODY_READ_FAILED", false)
			} else {
				writeRPCProblem(expected, test.err)
			}
			if !writeArtifactUploadError(actual, test.err) {
				t.Fatal("non-nil upload error was not handled")
			}
			assertReadRPCProjection(t, actual, expected)
			var body map[string]any
			if err := json.Unmarshal(actual.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if actual.Code != test.http || body["code"] != test.code {
				t.Fatal("upload failure precedence changed")
			}
		})
	}
}

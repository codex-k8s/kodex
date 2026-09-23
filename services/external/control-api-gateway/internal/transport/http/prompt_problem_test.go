package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestWritePromptProblemPreservesOnlyBoundedDiagnostics(t *testing.T) {
	base := status.New(codes.InvalidArgument, "private upstream message")
	upstream, err := base.WithDetails(&cp.PromptTemplateErrorDetail{Diagnostics: []*cp.PromptTemplateDiagnostic{{
		Severity: "ERROR", Code: "PROMPT_TEMPLATE_VARIABLE_UNKNOWN",
		Message: "private diagnostic", Line: 2, Column: 4, VariableName: "unknown.value",
	}}})
	if err != nil {
		t.Fatalf("attach prompt detail: %v", err)
	}
	recorder := httptest.NewRecorder()
	writePromptProblem(recorder, upstream.Err())
	if recorder.Code != http.StatusBadRequest || strings.Contains(recorder.Body.String(), "private") {
		t.Fatalf("problem = %d %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Code        string `json:"code"`
		Diagnostics []struct {
			Code, Message, VariableName string
			Line, Column                int32
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if body.Code != promptTemplateInvalid || len(body.Diagnostics) != 1 || body.Diagnostics[0].Code != "PROMPT_TEMPLATE_VARIABLE_UNKNOWN" || body.Diagnostics[0].Message != "i18n:PROMPT_TEMPLATE_VARIABLE_UNKNOWN" || body.Diagnostics[0].VariableName != "unknown.value" || body.Diagnostics[0].Line != 2 || body.Diagnostics[0].Column != 4 {
		t.Fatalf("problem body = %#v", body)
	}
}

func TestWritePromptProblemRejectsMalformedTypedDetail(t *testing.T) {
	for name, detail := range map[string]*cp.PromptTemplateErrorDetail{
		"empty":            {},
		"invalid variable": {Diagnostics: []*cp.PromptTemplateDiagnostic{{Severity: "ERROR", Code: "PROMPT_TEMPLATE_VARIABLE_UNKNOWN", Message: "safe", Line: 1, Column: 1, VariableName: strings.Repeat("x", 161)}}},
	} {
		t.Run(name, func(t *testing.T) {
			upstream, err := status.New(codes.InvalidArgument, "invalid").WithDetails(detail)
			if err != nil {
				t.Fatalf("attach detail: %v", err)
			}
			recorder := httptest.NewRecorder()
			writePromptProblem(recorder, upstream.Err())
			if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), `"code":"INVALID_UPSTREAM_RESPONSE"`) {
				t.Fatalf("malformed detail accepted: %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
	t.Run("unknown detail", func(t *testing.T) {
		upstream, err := status.New(codes.InvalidArgument, "invalid").WithDetails(&emptypb.Empty{})
		if err != nil {
			t.Fatalf("attach detail: %v", err)
		}
		recorder := httptest.NewRecorder()
		writePromptProblem(recorder, upstream.Err())
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"INVALID_REQUEST"`) {
			t.Fatalf("ordinary invalid argument remapped: %d %s", recorder.Code, recorder.Body.String())
		}
	})
}

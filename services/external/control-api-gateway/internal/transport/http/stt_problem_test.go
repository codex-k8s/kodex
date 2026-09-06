package httptransport

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/sttapi/errorprofile"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestSpeechRoutesDoNotReplayRateLimitedTranscription(t *testing.T) {
	upstream, err := status.New(codes.ResourceExhausted, "private provider response").WithDetails(&errdetails.ErrorInfo{Domain: errorprofile.Domain, Reason: errorprofile.TranscriptionRateLimited}, &errdetails.RetryInfo{RetryDelay: &durationpb.Duration{Seconds: 17}})
	if err != nil {
		t.Fatal(err)
	}
	for _, organization := range []bool{false, true} {
		body := &bytes.Buffer{}
		form := multipart.NewWriter(body)
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="audio"; filename="recording.mp3"`)
		header.Set("Content-Type", "audio/mpeg")
		part, err := form.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("audio")); err != nil {
			t.Fatal(err)
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		client := &speechClientStub{stream: &speechStreamStub{recvErr: upstream.Err()}}
		server := &Server{speech: client}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/speech/transcriptions", body)
		request.Header.Set("Content-Type", form.FormDataContentType())
		response := httptest.NewRecorder()
		if organization {
			server.TranscribeOrganizationSpeech(response, request, generated.TranscribeOrganizationSpeechParams{XAudioSize: 5})
		} else {
			server.TranscribeSpeech(response, request, "prj_project01", generated.TranscribeSpeechParams{XAudioSize: 5})
		}
		if response.Code != 429 || client.calls != 1 || response.Header().Get("Retry-After") != "17" || !strings.Contains(response.Body.String(), errorprofile.TranscriptionRateLimited) {
			t.Fatalf("speech route lost typed denial or repeated request: organization=%v status=%d calls=%d", organization, response.Code, client.calls)
		}
	}
}

func TestSpeechRateLimitPreservesBoundedHintWithoutRetry(t *testing.T) {
	for _, tc := range []struct {
		name       string
		domain     string
		delay      *durationpb.Duration
		duplicate  bool
		wantHeader string
	}{
		{"valid", errorprofile.Domain, &durationpb.Duration{Seconds: 17}, false, "17"},
		{"maximum", errorprofile.Domain, &durationpb.Duration{Seconds: 300}, false, "300"},
		{"missing", errorprofile.Domain, nil, false, ""},
		{"huge", errorprofile.Domain, &durationpb.Duration{Seconds: 301}, false, ""},
		{"negative", errorprofile.Domain, &durationpb.Duration{Seconds: -1}, false, ""},
		{"fraction", errorprofile.Domain, &durationpb.Duration{Seconds: 1, Nanos: 1}, false, ""},
		{"duplicate", errorprofile.Domain, &durationpb.Duration{Seconds: 17}, true, ""},
		{"other_domain", "kodex.control-plane", &durationpb.Duration{Seconds: 17}, false, "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream, err := status.New(codes.ResourceExhausted, "private provider response").WithDetails(&errdetails.ErrorInfo{Domain: tc.domain, Reason: errorprofile.TranscriptionRateLimited})
			if err != nil {
				t.Fatal(err)
			}
			if tc.delay != nil {
				upstream, err = upstream.WithDetails(&errdetails.RetryInfo{RetryDelay: tc.delay})
				if err != nil {
					t.Fatal(err)
				}
				if tc.duplicate {
					upstream, err = upstream.WithDetails(&errdetails.RetryInfo{RetryDelay: tc.delay})
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, early := range []bool{false, true} {
				response := httptest.NewRecorder()
				if early {
					writeSpeechSendProblem(response, &speechStreamStub{recvErr: upstream.Err()}, io.EOF)
				} else {
					writeSpeechProblem(response, upstream.Err())
				}
				var body struct {
					Code      string `json:"code"`
					Retryable bool   `json:"retryable"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				wantCode := errorprofile.TranscriptionRateLimited
				if tc.domain != errorprofile.Domain {
					wantCode = "RATE_LIMITED"
				}
				if response.Code != 429 || response.Header().Get("Retry-After") != tc.wantHeader || body.Code != wantCode || body.Retryable != (tc.wantHeader != "") || strings.Contains(response.Body.String(), "private provider") {
					t.Fatalf("invalid safe rate limit mapping: early=%v status=%d header=%q code=%q retryable=%v", early, response.Code, response.Header().Get("Retry-After"), body.Code, body.Retryable)
				}
			}
		})
	}
}

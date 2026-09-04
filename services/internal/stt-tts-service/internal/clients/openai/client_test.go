package openai

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (function roundTrip) Do(request *http.Request) (*http.Response, error) { return function(request) }

func TestTranscribeUsesPinnedRequest(t *testing.T) {
	client, err := NewWithHTTPClient(roundTrip(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.String() != Endpoint || request.Header.Get("Authorization") != "Bearer test-only" {
			t.Fatal("неверная HTTPS-граница")
		}
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if request.FormValue("model") != "gpt-transcribe" || request.FormValue("language") != "ru" {
			t.Fatal("неверная server-pinned конфигурация")
		}
		if len(request.MultipartForm.Value) != 2 || len(request.MultipartForm.File) != 1 {
			t.Fatal("обнаружен незаявленный параметр provider")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"text":"раз два три","usage":{"type":"tokens","total_tokens":3}}`))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.Transcribe(t.Context(), value.ProviderRequest{
		Model: "gpt-transcribe", Language: "ru", APIKey: []byte("test-only"),
		Audio: value.Audio{MediaType: "audio/mpeg", Bytes: []byte("audio")},
	})
	if err != nil || text != "раз два три" {
		t.Fatalf("неожиданный результат: %q, %v", text, err)
	}
}

func TestTranscribeRejectsProviderDiagnostics(t *testing.T) {
	client, _ := NewWithHTTPClient(roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader(`{"error":"sensitive"}`))}, nil
	}))
	_, err := client.Transcribe(t.Context(), value.ProviderRequest{
		Model: "gpt-transcribe", Language: "ru", APIKey: []byte("test-only"),
		Audio: value.Audio{MediaType: "audio/mpeg", Bytes: []byte("audio")},
	})
	if err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("provider diagnostics leaked: %v", err)
	}
}

func TestCheckUsesBodylessExactEgressReadiness(t *testing.T) {
	client, _ := NewWithHTTPClient(roundTrip(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.String() != ProxyURL+"/readyz" || request.Body != nil {
			t.Fatal("неверная readiness-граница")
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	if err := client.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
}

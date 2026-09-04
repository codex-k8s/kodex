package openai

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (function doerFunc) Do(request *http.Request) (*http.Response, error) { return function(request) }

func TestTranscribeStreamsExactPinnedMultipart(t *testing.T) {
	audio := bytes.Repeat([]byte{0x5a}, 1<<20)
	client, err := NewWithHTTPClient(doerFunc(func(request *http.Request) (*http.Response, error) {
		mediaType, parameters, parseErr := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if parseErr != nil || mediaType != "multipart/form-data" || parameters["boundary"] != multipartBoundary {
			t.Fatalf("content type = %q, %v", mediaType, parseErr)
		}
		reader := multipart.NewReader(request.Body, parameters["boundary"])
		fields := map[string]string{}
		var fileBytes int
		for {
			part, partErr := reader.NextPart()
			if partErr == io.EOF {
				break
			}
			if partErr != nil {
				t.Fatal(partErr)
			}
			raw, readErr := io.ReadAll(part)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if part.FormName() == "file" {
				fileBytes = len(raw)
			} else {
				fields[part.FormName()] = string(raw)
			}
		}
		if fileBytes != len(audio) || len(fields) != 2 || fields["model"] != value.DefaultModel || fields["language"] != value.DefaultLanguage {
			t.Fatalf("multipart fields=%v file=%d", fields, fileBytes)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"text":"ok","usage":{}}`))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	text, err := client.Transcribe(t.Context(), value.ProviderRequest{
		Audio: value.Audio{Reader: bytes.NewReader(audio), SizeBytes: int64(len(audio)), MediaType: "audio/mpeg", FileName: "audio.mp3"},
		Model: value.DefaultModel, Language: value.DefaultLanguage, APIKey: []byte("test-only-key"),
	})
	if err != nil || text != "ok" {
		t.Fatalf("result=%q err=%v", text, err)
	}
}

func TestTranscribeClassifiesNetworkFailureAsEgress(t *testing.T) {
	client, _ := NewWithHTTPClient(doerFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("network detail") }))
	_, err := client.Transcribe(t.Context(), value.ProviderRequest{
		Audio: value.Audio{Reader: bytes.NewReader([]byte("data")), SizeBytes: 4, FileName: "audio.mp3"},
		Model: value.DefaultModel, Language: value.DefaultLanguage, APIKey: []byte("test-only-key"),
	})
	if !errors.Is(err, errs.ErrEgressUnavailable) {
		t.Fatalf("classification=%v", err)
	}
}

func TestReadinessIsLocalAndEgressIsDiagnostic(t *testing.T) {
	calls := 0
	client, _ := NewWithHTTPClient(doerFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	if err := client.CheckLocal(t.Context()); err != nil || calls != 0 {
		t.Fatalf("local check: calls=%d err=%v", calls, err)
	}
	if err := client.CheckEgress(t.Context()); err != nil || calls != 1 {
		t.Fatalf("diagnostic: calls=%d err=%v", calls, err)
	}
}

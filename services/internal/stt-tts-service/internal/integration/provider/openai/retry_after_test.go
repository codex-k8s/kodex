package openai

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

func TestRetryAfterPreservesOnlyBoundedMinimum(t *testing.T) {
	now := time.Date(2026, 9, 6, 10, 0, 0, 500000000, time.UTC)
	for _, test := range []struct {
		values   []string
		expected time.Duration
	}{
		{[]string{"17"}, 17 * time.Second}, {[]string{"300"}, 300 * time.Second},
		{[]string{now.Add(3 * time.Second).UTC().Format(http.TimeFormat)}, 3 * time.Second},
		{nil, 0}, {[]string{""}, 0}, {[]string{"0"}, 0}, {[]string{"-1"}, 0}, {[]string{"1.5"}, 0},
		{[]string{"301"}, 0}, {[]string{strings.Repeat("9", 128)}, 0}, {[]string{"17", "18"}, 0},
		{[]string{"garbage"}, 0}, {[]string{now.Add(time.Hour).Format(http.TimeFormat)}, 0}, {[]string{now.Add(-time.Second).Format(http.TimeFormat)}, 0},
	} {
		if actual := boundedRetryAfter(http.Header{"Retry-After": test.values}, now); actual != test.expected {
			t.Fatal("retry hint violated bounded minimum")
		}
	}
}

func TestProvider429IsTypedAndNeverRetriesOrReadsPrivateBody(t *testing.T) {
	calls := 0
	client, err := NewWithHTTPClient(doerFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodPost || request.URL.String() != Endpoint {
			t.Fatal("unexpected provider effect")
		}
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"17"}}, Body: io.NopCloser(strings.NewReader("private provider response"))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	audio := []byte("fixture")
	_, err = client.Transcribe(t.Context(), value.ProviderRequest{Audio: value.Audio{Reader: bytes.NewReader(audio), SizeBytes: int64(len(audio)), FileName: "audio.mp3"}, Model: value.DefaultModel, Language: value.DefaultLanguage, APIKey: []byte("test-only-key")})
	var limited *errs.ProviderRateLimit
	if calls != 1 || !errors.As(err, &limited) || limited.RetryAfter != 17*time.Second || strings.Contains(err.Error(), "private") {
		t.Fatal("provider rate limit lost safe single-effect semantics")
	}
}

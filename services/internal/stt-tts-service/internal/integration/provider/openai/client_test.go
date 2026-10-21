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

// doerFunc injects a request assertion and synthetic response without making an external HTTP call.
// doerFunc внедряет проверку запроса и синтетический ответ без внешнего HTTP-вызова.
type doerFunc func(*http.Request) (*http.Response, error)

// Do forwards the original request to the test callback without transport policy or response rewriting.
// Do передаёт исходный запрос тестовому callback без транспортной политики или преобразования ответа.
func (function doerFunc) Do(request *http.Request) (*http.Response, error) { return function(request) }

// TestTranscribeStreamsExactPinnedMultipart parses a one-MiB synthetic request and verifies the boundary and exact catalog fields.
// Its fake HTTP executor checks adapter serialization, not provider recognition or live TLS connectivity.
// TestTranscribeStreamsExactPinnedMultipart разбирает синтетический запрос в один МиБ и проверяет границу и точные поля каталога.
// Подставной HTTP-исполнитель проверяет сериализацию адаптера, не распознавание провайдером или живое TLS-соединение.
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
		if fileBytes != len(audio) || len(fields) != 4 || fields["model"] != value.DefaultModel || fields["languages[]"] != value.DefaultLanguage || fields["response_format"] != "json" || fields["temperature"] != "0" {
			t.Fatal("multipart fields mismatch")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"text":"ok","usage":{},"languages":[{"code":"ru"}]}`))}, nil
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

// TestModelProbeIsExactNonBillableAndFailClosed requires one metadata-only GET and rejects wrong IDs, objects, statuses, and oversized bodies.
// Unsupported local model IDs must fail without increasing the fake HTTP call count.
// TestModelProbeIsExactNonBillableAndFailClosed требует один GET метаданных и отвергает неверные ID, типы, статусы и чрезмерные тела.
// Неподдержанный локальный ID модели должен отклоняться без увеличения числа подставных HTTP-вызовов.
func TestModelProbeIsExactNonBillableAndFailClosed(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		ready  bool
	}{
		{200, `{"id":"gpt-transcribe","object":"model"}`, true},
		{200, `{"id":"other","object":"model"}`, false},
		{200, `{"id":"gpt-transcribe","object":"other"}`, false},
		{200, `{}`, false}, {401, `{}`, false}, {429, `{}`, false}, {500, `{}`, false},
		{200, strings.Repeat("x", 16385), false},
	} {
		calls := 0
		client, _ := NewWithHTTPClient(doerFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			if request.Method != http.MethodGet || request.URL.String() != "https://api.openai.com/v1/models/gpt-transcribe" || request.Body != nil {
				t.Fatal("probe выполнил неверный запрос")
			}
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		}))
		err := client.CheckModel(t.Context(), value.DefaultModel, []byte("test-only-key"))
		if (err == nil) != tc.ready || calls != 1 {
			t.Fatal("probe fail-closed/retry contract нарушен")
		}
		if err := client.CheckModel(t.Context(), "unknown", []byte("test-only-key")); err == nil || calls != 1 {
			t.Fatal("unknown model достиг сети")
		}
	}
}

// TestProductionTransportPinsTLSAndEgress inspects the configured proxy, TLS 1.3 bounds, certificate checks, and redirect rejection without dialing.
// TestProductionTransportPinsTLSAndEgress проверяет настройки прокси, границы TLS 1.3, проверку сертификата и запрет redirect без соединения.
func TestProductionTransportPinsTLSAndEgress(t *testing.T) {
	client, err := New(testEgressConfig())
	if err != nil {
		t.Fatal(err)
	}
	httpClient := client.http.(*http.Client)
	transport := httpClient.Transport.(*http.Transport)
	request, _ := http.NewRequest(http.MethodGet, Endpoint, nil)
	proxy, err := transport.Proxy(request)
	if err != nil || proxy.String() != ProxyURL || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.MinVersion != 0x0304 || transport.TLSClientConfig.MaxVersion != 0x0304 {
		t.Fatal("TLS/egress weakened")
	}
	if httpClient.CheckRedirect(request, nil) == nil {
		t.Fatal("redirect разрешён")
	}
}

// TestTranscribeClassifiesNetworkFailureAsEgress maps an injected HTTP transport failure to the egress sentinel.
// TestTranscribeClassifiesNetworkFailureAsEgress сопоставляет внедрённую ошибку HTTP-транспорта с признаком отказа egress.
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

// TestReadinessIsLocalAndEgressIsDiagnostic proves local readiness performs no HTTP call and explicit proxy diagnostics perform exactly one.
// TestReadinessIsLocalAndEgressIsDiagnostic доказывает отсутствие HTTP в локальной готовности и ровно один вызов явной диагностики прокси.
func TestReadinessIsLocalAndEgressIsDiagnostic(t *testing.T) {
	calls := 0
	client, _ := NewWithHTTPClient(doerFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusNoContent, Header: testEgressHeaders(), Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	client.egress = testEgressConfig()
	if err := client.CheckLocal(t.Context()); err != nil || calls != 0 {
		t.Fatalf("local check: calls=%d err=%v", calls, err)
	}
	if err := client.CheckEgress(t.Context()); err != nil || calls != 1 {
		t.Fatalf("diagnostic: calls=%d err=%v", calls, err)
	}
}

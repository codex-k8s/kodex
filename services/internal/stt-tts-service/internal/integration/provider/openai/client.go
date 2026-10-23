// Package openai implements the fixed HTTPS transcription route and its pinned proxy readback.
// Пакет openai реализует фиксированный HTTPS-маршрут распознавания и сверку ожидаемого прокси.
package openai

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

const (
	// Endpoint fixes the transcription destination; caller input cannot select another URL.
	// Endpoint фиксирует адрес распознавания; входные данные не выбирают другой URL.
	Endpoint = "https://api.openai.com/v1/audio/transcriptions"
	// ProxyURL selects the internal CONNECT proxy and its separate local diagnostic endpoint.
	// ProxyURL выбирает внутренний CONNECT-прокси и его отдельную локальную диагностическую точку.
	ProxyURL = "http://egress-gateway.kodex-system.svc.cluster.local:8081"
	// maximumResponseBytes bounds successful JSON bodies; it is this adapter's limit, not a provider quota.
	// maximumResponseBytes ограничивает успешное JSON-тело; это лимит адаптера, не квота провайдера.
	maximumResponseBytes = 1 << 20
	// multipartBoundary makes envelope construction deterministic without buffering the audio payload.
	// multipartBoundary делает сборку оболочки детерминированной без буферизации самого аудио.
	multipartBoundary = "kodex-stt-boundary-v1"
)

// doer abstracts one HTTP exchange so adapter tests can inspect requests without external traffic.
// doer абстрагирует один HTTP-обмен для проверки запросов без внешнего трафика в тестах адаптера.
type doer interface {
	// Do executes the request under its context; callers close any successfully returned response body.
	// Do исполняет запрос с его контекстом; вызывающий закрывает тело успешно возвращённого ответа.
	Do(*http.Request) (*http.Response, error)
}

// Client separates provider traffic from proxy diagnostics and retains the expected egress generation.
// Client отделяет трафик провайдера от диагностики прокси и хранит ожидаемое поколение egress.
type Client struct {
	http      doer
	readiness doer
	egress    EgressConfig
}

// New constructs a TLS-1.3-only provider client with exact CONNECT readback and no redirects.
// Construction performs no network probe; the separate internal diagnostic client does not use CONNECT.
// New создаёт клиент провайдера только с TLS 1.3, точной сверкой CONNECT и запретом перенаправлений.
// Создание не проверяет сеть; отдельный клиент внутренней диагностики не использует CONNECT.
func New(config EgressConfig) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	proxy, err := url.Parse(ProxyURL)
	if err != nil || proxy.String() != ProxyURL {
		return nil, errors.New("OpenAI egress proxy configuration is invalid")
	}
	client, err := NewWithHTTPClient(&http.Client{Transport: &http.Transport{
		Proxy: http.ProxyURL(proxy), TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13},
		OnProxyConnectResponse: config.onConnect,
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("OpenAI redirect is rejected") }})
	if err != nil {
		return nil, err
	}
	client.egress = config
	client.readiness = &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("STT egress readiness redirect is rejected")
	}}
	return client, nil
}

// NewWithHTTPClient injects a non-nil HTTP executor for hermetic tests; it does not install production TLS or proxy policy.
// NewWithHTTPClient внедряет непустой HTTP-исполнитель для изолированных тестов; production TLS и политика прокси здесь не устанавливаются.
func NewWithHTTPClient(client doer) (*Client, error) {
	if client == nil {
		return nil, errors.New("OpenAI HTTP client is required")
	}
	return &Client{http: client, readiness: client}, nil
}

// CheckLocal checks only context presence and configured client fields, without network I/O or a cancellation probe.
// CheckLocal проверяет только наличие контекста и полей клиента, без сети и проверки отмены контекста.
func (client *Client) CheckLocal(ctx context.Context) error {
	if ctx == nil || client == nil || client.http == nil || client.readiness == nil {
		return errors.New("OpenAI provider adapter is not configured")
	}
	return nil
}

// CheckEgress requires the proxy's 204 diagnostic response and every exact expected generation header.
// This optional diagnostic neither authenticates the provider key nor serves as Kubernetes readiness.
// CheckEgress требует диагностический ответ прокси 204 и все точно ожидаемые заголовки поколения.
// Необязательная диагностика не проверяет ключ провайдера и не является Kubernetes readiness.
func (client *Client) CheckEgress(ctx context.Context) error {
	if err := client.CheckLocal(ctx); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, ProxyURL+"/readyz", nil)
	if err != nil {
		return errors.New("create STT egress diagnostic request")
	}
	response, err := client.readiness.Do(request)
	if err != nil {
		return errs.ErrEgressUnavailable
	}
	defer response.Body.Close()
	if client.egress.check(response.Header) != nil || response.StatusCode != http.StatusNoContent {
		return errs.ErrEgressUnavailable
	}
	return nil
}

// Transcribe validates the catalog profile, rewinds bounded audio, and submits one non-streaming multipart request.
// It never retries, discards error bodies, bounds and strictly decodes successful JSON, and retains cancellation causes.
// Transcribe проверяет профиль каталога, перематывает ограниченное аудио и делает один multipart-запрос без потокового ответа.
// Повторов нет; тела ошибок отбрасываются, успешный JSON ограничивается и строго разбирается, причины отмены сохраняются.
func (client *Client) Transcribe(ctx context.Context, request value.ProviderRequest) (string, error) {
	if ctx == nil || modelprofile.Validate(request.Model, request.Language, request.Parameters) != nil || len(request.APIKey) == 0 ||
		request.Audio.Reader == nil || request.Audio.SizeBytes <= 0 || request.Audio.SizeBytes > value.MaximumAbsoluteBytes {
		return "", errs.ErrInvalidRequest
	}
	if _, err := request.Audio.Reader.Seek(0, io.SeekStart); err != nil {
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("seek OpenAI audio input"))
	}
	prefix, suffix, contentType, err := multipartEnvelope(request.Audio.FileName, request.Model, request.Language, request.Parameters)
	if err != nil {
		return "", errors.Join(errs.ErrProviderUnavailable, err)
	}
	contentLength := int64(prefix.Len()+suffix.Len()) + request.Audio.SizeBytes
	body := io.MultiReader(bytes.NewReader(prefix.Bytes()), io.LimitReader(request.Audio.Reader, request.Audio.SizeBytes), bytes.NewReader(suffix.Bytes()))
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, Endpoint, body)
	if err != nil {
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("create OpenAI transcription request"))
	}
	httpRequest.ContentLength = contentLength
	httpRequest.Header.Set("Authorization", "Bearer "+string(request.APIKey))
	httpRequest.Header.Set("Content-Type", contentType)
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", "kodex-stt-tts-service")
	response, err := client.http.Do(httpRequest)
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.Join(errs.ErrProviderUnavailable, ctx.Err())
		}
		return "", errors.Join(errs.ErrEgressUnavailable, errors.New("OpenAI transcription request failed"))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusTooManyRequests:
			return "", &errs.ProviderRateLimit{RetryAfter: boundedRetryAfter(response.Header, time.Now())}
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest, http.StatusNotFound:
			return "", errs.ErrProviderRejected
		default:
			return "", errs.ErrProviderUnavailable
		}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil || len(raw) > maximumResponseBytes {
		if ctx.Err() != nil {
			return "", errors.Join(errs.ErrProviderUnavailable, ctx.Err())
		}
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("read OpenAI transcription response"))
	}
	var result struct {
		Text      string          `json:"text"`
		Usage     json.RawMessage `json:"usage"`
		Languages []struct {
			Code string `json:"code"`
		} `json:"languages"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || strings.TrimSpace(result.Text) == "" {
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("decode OpenAI transcription response"))
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("OpenAI transcription response contains trailing data"))
	}
	return strings.TrimSpace(result.Text), nil
}

// switchingWriter redirects multipart metadata into prefix or suffix buffers while leaving audio outside both.
// switchingWriter направляет multipart-метаданные в буфер префикса либо суффикса, не помещая туда аудио.
type switchingWriter struct {
	current io.Writer
}

// Write delegates to the currently selected buffer and preserves its byte count and error.
// Write передаёт запись выбранному буферу и сохраняет число байтов и ошибку.
func (writer *switchingWriter) Write(input []byte) (int, error) {
	return writer.current.Write(input)
}

// multipartEnvelope builds the exact file header and trailing catalog fields around separately streamed audio.
// It rejects header-breaking filenames; full model/parameter eligibility remains the caller's prior validation.
// multipartEnvelope строит точный заголовок файла и последующие поля каталога вокруг отдельно передаваемого аудио.
// Имена, нарушающие заголовок, отвергаются; полный допуск модели и параметров предварительно проверяет вызывающий.
func multipartEnvelope(fileName, model, language string, parameters modelprofile.Parameters) (*bytes.Buffer, *bytes.Buffer, string, error) {
	if fileName == "" || strings.ContainsAny(fileName, "\r\n\"") {
		return nil, nil, "", errors.New("OpenAI audio filename is invalid")
	}
	prefix, suffix := &bytes.Buffer{}, &bytes.Buffer{}
	destination := &switchingWriter{current: prefix}
	writer := multipart.NewWriter(destination)
	if err := writer.SetBoundary(multipartBoundary); err != nil {
		return nil, nil, "", errors.New("set OpenAI multipart boundary")
	}
	if _, err := writer.CreateFormFile("file", fileName); err != nil {
		return nil, nil, "", errors.New("create OpenAI multipart file")
	}
	destination.current = suffix
	if err := writer.WriteField("model", model); err != nil {
		return nil, nil, "", errors.New("write OpenAI model field")
	}
	fields := [][2]string{{"temperature", strconv.FormatFloat(parameters.Temperature, 'f', -1, 64)}}
	if model == modelprofile.RecommendedModel {
		languages := parameters.Languages
		if language != "" {
			languages = []string{language}
		}
		for _, hint := range languages {
			fields = append(fields, [2]string{"languages[]", hint})
		}
	} else if language != "" {
		fields = append(fields, [2]string{"language", language})
	}
	for _, keyword := range parameters.Keywords {
		fields = append(fields, [2]string{"keywords[]", keyword})
	}
	if parameters.Prompt != "" {
		fields = append(fields, [2]string{"prompt", parameters.Prompt})
	}
	if parameters.ChunkingStrategy != "" {
		fields = append(fields, [2]string{"chunking_strategy", parameters.ChunkingStrategy})
	}
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return nil, nil, "", errors.New("write OpenAI transcription parameter")
		}
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return nil, nil, "", errors.New("write OpenAI response format")
	}
	if err := writer.Close(); err != nil {
		return nil, nil, "", errors.New("close OpenAI multipart body")
	}
	return prefix, suffix, writer.FormDataContentType(), nil
}

// CheckModel issues one bounded model-metadata GET for a supported catalog ID, without audio or transcription POST.
// Matching metadata and a still-live context prove only this probe, not a successful transcription or account quota.
// CheckModel делает один ограниченный GET метаданных поддержанной модели без аудио и POST распознавания.
// Совпадение метаданных и живой контекст доказывают лишь этот запрос, не успешное распознавание или квоту аккаунта.
func (client *Client) CheckModel(ctx context.Context, model string, key []byte) error {
	_, supported := modelprofile.Lookup(model)
	if ctx == nil || !supported || len(key) < 8 || len(key) > 16<<10 {
		return errs.ErrInvalidRequest
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.openai.com/v1/models/"+model, nil)
	if err != nil {
		return errs.ErrInvalidRequest
	}
	request.Header.Set("Authorization", "Bearer "+string(key))
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errs.ErrEgressUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errs.ErrProviderUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(raw) > 16384 {
		return errs.ErrProviderUnavailable
	}
	var result struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	}
	if json.Unmarshal(raw, &result) != nil || result.ID != model || result.Object != "model" {
		return errs.ErrProviderUnavailable
	}
	return ctx.Err()
}

// Catalog returns the shared local OpenAI model profile; it neither queries the network nor proves account access.
// Catalog возвращает общий локальный профиль моделей OpenAI, не обращаясь в сеть и не доказывая доступ аккаунта.
func (client *Client) Catalog() modelprofile.Catalog { return modelprofile.OpenAICatalog() }

// Package openai реализует единственный исходящий HTTPS adapter распознавания.
package openai

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

const (
	Endpoint             = "https://api.openai.com/v1/audio/transcriptions"
	ProxyURL             = "http://egress-gateway.kodex-system.svc.cluster.local:8080"
	maximumResponseBytes = 1 << 20
)

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Client struct {
	http      doer
	readiness doer
}

func New() (*Client, error) {
	proxy, err := url.Parse(ProxyURL)
	if err != nil || proxy.String() != ProxyURL {
		return nil, errors.New("OpenAI egress proxy configuration is invalid")
	}
	client, err := NewWithHTTPClient(&http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxy),
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13},
	}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("OpenAI redirect is rejected")
	}})
	if err != nil {
		return nil, err
	}
	client.readiness = &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("STT egress readiness redirect is rejected")
	}}
	return client, nil
}

// NewWithHTTPClient предназначен для hermetic adapter-тестов.
func NewWithHTTPClient(client doer) (*Client, error) {
	if client == nil {
		return nil, errors.New("OpenAI HTTP client is required")
	}
	return &Client{http: client, readiness: client}, nil
}

func (client *Client) Check(ctx context.Context) error {
	if client == nil || client.http == nil || client.readiness == nil {
		return errors.New("OpenAI provider adapter is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, ProxyURL+"/readyz", nil)
	if err != nil {
		return errors.New("create STT egress readiness request")
	}
	response, err := client.readiness.Do(request)
	if err != nil {
		return errors.New("STT egress gateway is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return errors.New("STT egress gateway is not ready")
	}
	return nil
}

func (client *Client) Transcribe(ctx context.Context, request value.ProviderRequest) (string, error) {
	if request.Model != value.DefaultModel || request.Language != value.DefaultLanguage || len(request.APIKey) == 0 || len(request.Audio.Bytes) == 0 {
		return "", errs.ErrInvalidRequest
	}
	body := bytes.NewBuffer(make([]byte, 0, len(request.Audio.Bytes)+1024))
	writer := multipart.NewWriter(body)
	file, err := writer.CreateFormFile("file", filename(request.Audio.MediaType))
	if err != nil {
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("create OpenAI multipart file"))
	}
	if _, err := file.Write(request.Audio.Bytes); err != nil {
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("write OpenAI multipart file"))
	}
	if err := writer.WriteField("model", request.Model); err != nil {
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("write OpenAI model field"))
	}
	if err := writer.WriteField("language", request.Language); err != nil {
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("write OpenAI language field"))
	}
	if err := writer.Close(); err != nil {
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("close OpenAI multipart body"))
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, Endpoint, body)
	if err != nil {
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("create OpenAI transcription request"))
	}
	httpRequest.Header.Set("Authorization", "Bearer "+string(request.APIKey))
	httpRequest.Header.Set("Content-Type", writer.FormDataContentType())
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", "kodex-stt-tts-service")
	response, err := client.http.Do(httpRequest)
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.Join(errs.ErrProviderUnavailable, ctx.Err())
		}
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("OpenAI transcription request failed"))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
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
		Text  string          `json:"text"`
		Usage json.RawMessage `json:"usage"`
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

func filename(mediaType string) string {
	switch mediaType {
	case "audio/mpeg":
		return "audio.mp3"
	case "audio/wav":
		return "audio.wav"
	case "audio/flac":
		return "audio.flac"
	default:
		return "audio.bin"
	}
}

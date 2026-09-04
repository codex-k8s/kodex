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
	multipartBoundary    = "kodex-stt-boundary-v1"
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
		Proxy: http.ProxyURL(proxy), TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13},
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("OpenAI redirect is rejected") }})
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

// CheckLocal не обращается к сети и проверяет только локальную конфигурацию.
func (client *Client) CheckLocal(ctx context.Context) error {
	if ctx == nil || client == nil || client.http == nil || client.readiness == nil {
		return errors.New("OpenAI provider adapter is not configured")
	}
	return nil
}

// CheckEgress — отдельный diagnostic readback, не Kubernetes readiness.
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
	if response.StatusCode != http.StatusNoContent {
		return errs.ErrEgressUnavailable
	}
	return nil
}

func (client *Client) Transcribe(ctx context.Context, request value.ProviderRequest) (string, error) {
	if ctx == nil || request.Model != value.DefaultModel || request.Language != value.DefaultLanguage || len(request.APIKey) == 0 ||
		request.Audio.Reader == nil || request.Audio.SizeBytes <= 0 || request.Audio.SizeBytes > value.MaximumAbsoluteBytes {
		return "", errs.ErrInvalidRequest
	}
	if _, err := request.Audio.Reader.Seek(0, io.SeekStart); err != nil {
		return "", errors.Join(errs.ErrProviderUnavailable, errors.New("seek OpenAI audio input"))
	}
	prefix, suffix, contentType, err := multipartEnvelope(request.Audio.FileName, request.Model, request.Language)
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

type switchingWriter struct {
	current io.Writer
}

func (writer *switchingWriter) Write(input []byte) (int, error) {
	return writer.current.Write(input)
}

func multipartEnvelope(fileName, model, language string) (*bytes.Buffer, *bytes.Buffer, string, error) {
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
	if err := writer.WriteField("language", language); err != nil {
		return nil, nil, "", errors.New("write OpenAI language field")
	}
	if err := writer.Close(); err != nil {
		return nil, nil, "", errors.New("close OpenAI multipart body")
	}
	return prefix, suffix, writer.FormDataContentType(), nil
}

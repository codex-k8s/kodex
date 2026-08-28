// Package callback реализует узкий execution-scoped client к runtime-controller.
package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

type Client struct {
	http  *http.Client
	base  *url.URL
	token string
}

func New(input model.Input) (*Client, error) {
	transport, err := exactTransport(input.CallbackTLS)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(input.CallbackURL)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, errors.New("parse runtime callback URL")
	}
	token, err := readCredential(input.ExecutionTicketFile)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	return &Client{http: &http.Client{Transport: transport, Timeout: 35 * time.Second}, base: base, token: token}, nil
}

func (client *Client) Close() {
	if transport, ok := client.http.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}
func (client *Client) Token() string { return client.token }

func (client *Client) Progress(ctx context.Context, input model.Input, code string) error {
	return client.post(ctx, "/v1/executions/"+url.PathEscape(input.LeaseRef)+"/progress", runtimecontract.RunnerProgressRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Progress: code})
}

func (client *Client) Complete(ctx context.Context, input model.Input, payload runtimecontract.RunnerCompletionRequest) error {
	return client.post(ctx, "/v1/executions/"+url.PathEscape(input.LeaseRef)+"/complete", payload)
}

func (client *Client) ReadArtifact(ctx context.Context, input model.Input, artifact runtimecontract.RunnerInputArtifact) ([]byte, error) {
	endpoint := *client.base
	endpoint.Path = "/v1/executions/" + url.PathEscape(input.LeaseRef) + "/artifacts/" + url.PathEscape(artifact.Ref)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("create runtime artifact request")
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Accept", artifact.MediaType)
	response, err := client.http.Do(request)
	if err != nil {
		return nil, errors.New("runtime artifact callback is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != artifact.MediaType ||
		response.Header.Get("X-Kodex-Artifact-Digest") != artifact.Digest || response.ContentLength != artifact.SizeBytes {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
		return nil, errors.New("runtime artifact callback rejected request")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, runtimecontract.MaximumInputArtifactBytes+1))
	if err != nil || len(raw) > runtimecontract.MaximumInputArtifactBytes || int64(len(raw)) != artifact.SizeBytes {
		return nil, errors.New("runtime artifact response is invalid")
	}
	digest := sha256.Sum256(raw)
	actualDigest := "sha256:" + hex.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(actualDigest), []byte(artifact.Digest)) != 1 {
		return nil, errors.New("runtime artifact digest is invalid")
	}
	return raw, nil
}

func (client *Client) NextWarm(ctx context.Context, input model.Input) (model.Input, bool, error) {
	endpoint := *client.base
	endpoint.Path = "/v1/warm/next"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return model.Input{}, false, errors.New("create warm runtime request")
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("X-Kodex-Runtime-Revision", input.RuntimeRevisionRef)
	request.Header.Set("X-Kodex-Runtime-Revision-Digest", input.RuntimeRevisionDigest)
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return model.Input{}, false, errors.New("warm runtime callback is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return model.Input{}, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return model.Input{}, false, errors.New("warm runtime callback rejected request")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, runtimecontract.MaximumRunnerInputBytes+1))
	if err != nil || len(raw) > runtimecontract.MaximumRunnerInputBytes {
		return model.Input{}, false, errors.New("warm runtime response is invalid")
	}
	turn, err := runtimecontract.DecodeRunnerInput(raw)
	if err != nil {
		return model.Input{}, false, errors.New("decode warm runtime turn")
	}
	if turn.Mode != runtimecontract.RunnerModeTurn || !turn.SystemAssistant {
		return model.Input{}, false, errors.New("warm runtime turn kind is invalid")
	}
	warmCompatibility, warmErr := runtimecontract.WarmCompatibilityDigest(input)
	if warmErr != nil {
		return model.Input{}, false, errors.New("warm runtime compatibility is invalid")
	}
	turnCompatibility, turnErr := runtimecontract.WarmCompatibilityDigest(turn)
	if turnErr != nil {
		return model.Input{}, false, errors.New("warm runtime turn compatibility is invalid")
	}
	if turnCompatibility != warmCompatibility {
		return model.Input{}, false, errors.New("warm runtime turn compatibility mismatch")
	}
	return turn, true, nil
}

func (client *Client) post(ctx context.Context, path string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > runtimecontract.MaximumCompletionBytes+1<<20 {
		return errors.New("encode runtime callback request")
	}
	endpoint := *client.base
	endpoint.Path = path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return errors.New("create runtime callback request")
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return errors.New("runtime callback is unavailable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("runtime callback rejected request with status " + strconv.Itoa(response.StatusCode))
	}
	return nil
}

func exactTransport(binding model.TLSBinding) (*http.Transport, error) {
	ca, err := os.ReadFile(binding.CAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, errors.New("read runtime callback CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse runtime callback CA")
	}
	certificate, err := tls.LoadX509KeyPair(binding.CertificateFile, binding.PrivateKeyFile)
	if err != nil {
		return nil, errors.New("load runtime callback client identity")
	}
	return &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ServerName: binding.ServerName, RootCAs: roots, Certificates: []tls.Certificate{certificate}}, DisableCompression: true, MaxIdleConns: 2, MaxIdleConnsPerHost: 2, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 30 * time.Second, MaxResponseHeaderBytes: 16 << 10}, nil
}

func readCredential(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) != 64 {
		return "", errors.New("read execution ticket")
	}
	value := strings.TrimSpace(string(raw))
	if len(value) != 64 || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("execution ticket is invalid")
	}
	for index := range raw {
		raw[index] = 0
	}
	return value, nil
}

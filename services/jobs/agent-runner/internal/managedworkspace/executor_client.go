package managedworkspace

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// ExecutorClient owns one fixed local UDS transport. It has no endpoint parameter, ambient proxy,
// bearer, redirects or retry loop; unknown write responses must be reconciled by the caller.
// ExecutorClient владеет одним fixed local UDS transport. Endpoint parameter, ambient proxy,
// bearer, redirects и retry loop отсутствуют; неизвестный write response сверяет caller.
type ExecutorClient struct {
	transport *http.Transport
	client    *http.Client
}

// NewExecutorClient allocates only the trusted role's fixed local transport without dialing it.
// NewExecutorClient создаёт лишь fixed local transport доверенной роли без подключения к нему.
func NewExecutorClient() *ExecutorClient {
	transport := &http.Transport{DisableCompression: true, MaxConnsPerHost: 1, MaxResponseHeaderBytes: 8192, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", ExecutorSocket)
	}}
	client := &http.Client{Transport: transport, Timeout: (maximumCommandSeconds + 10) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errExecutor }}
	return &ExecutorClient{transport: transport, client: client}
}

// Call sends one request exactly once and accepts a bounded closed result. Cancellation or lost
// response is an error; it is never converted into a successful exit or repeated command.
// Call отправляет запрос ровно один раз и принимает bounded closed result. Cancel либо потерянный
// response — ошибка, никогда не успешный exit и не повторная команда.
func (client *ExecutorClient) Call(ctx context.Context, value ExecutorRequest) (CommandResult, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 64<<10 {
		return CommandResult{}, errExecutor
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://kodex-workspace-executor/execute", bytes.NewReader(raw))
	if err != nil {
		return CommandResult{}, errExecutor
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return CommandResult{}, errExecutor
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(response.Body, maximumCommandOutput*6+4097))
	var result CommandResult
	if err != nil || response.StatusCode != http.StatusOK || len(raw) > maximumCommandOutput*6+4096 || internalrpcauth.DecodeStrictJSON(raw, &result) != nil {
		return CommandResult{}, errExecutor
	}
	return result, nil
}

// Close releases idle local connections after the owner has obtained explicit quiescence.
// Close освобождает idle local connections после явного подтверждения owner quiescence.
func (client *ExecutorClient) Close() { client.transport.CloseIdleConnections() }

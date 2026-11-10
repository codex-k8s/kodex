package managedworkspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// ExecutorClient owns one fixed local UDS transport. It has no endpoint parameter, ambient proxy,
// bearer or redirects. Only connection setup may wait; an HTTP request is never repeated.
// ExecutorClient владеет одним fixed local UDS transport. Endpoint parameter, ambient proxy,
// bearer и redirects отсутствуют. Ждать может только установка соединения; HTTP-запрос не повторяется.
type ExecutorClient struct {
	transport *http.Transport
	client    *http.Client
}

// NewExecutorClient allocates only the trusted role's fixed local transport without dialing it.
// NewExecutorClient создаёт лишь fixed local transport доверенной роли без подключения к нему.
func NewExecutorClient() *ExecutorClient { return newExecutorClient(ExecutorSocket) }

// newExecutorClient keeps the production socket fixed while tests own an isolated Unix listener.
// newExecutorClient сохраняет fixed production socket; тесты используют собственный Unix listener.
func newExecutorClient(socket string) *ExecutorClient {
	transport := &http.Transport{DisableCompression: true, MaxConnsPerHost: 1, MaxResponseHeaderBytes: 8192, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialExecutor(ctx, socket)
	}}
	client := &http.Client{Transport: transport, Timeout: (maximumCommandSeconds + 10) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errExecutor }}
	return &ExecutorClient{transport: transport, client: client}
}

// dialExecutor waits at most three seconds for an independently starting local listener.
// Only ENOENT/ECONNREFUSED before a connection are retried; cancellation and other errors stop.
// No HTTP bytes have been sent, so this cannot replay an INITIALIZE, RUN or QUIESCE operation.
// dialExecutor ждёт локальный listener не более трёх секунд. Повторяется лишь connect после
// ENOENT/ECONNREFUSED; отмена и остальные ошибки завершают ожидание. HTTP bytes ещё не отправлены,
// поэтому повтор INITIALIZE, RUN либо QUIESCE невозможен.
func dialExecutor(parent context.Context, socket string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	for {
		connection, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
		if err == nil {
			return connection, nil
		}
		if !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
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
	// Transport must not recreate a command body after an uncertain send.
	// Transport не должен восстанавливать body команды после неопределённой отправки.
	request.GetBody = nil
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

package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// workspaceConnectDelays bounds cold network convergence to four waits without replaying an HTTP request.
// workspaceConnectDelays ограничивает ожидание сетевых правил четырьмя задержками без повтора HTTP-запроса.
var workspaceConnectDelays = [...]time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}

// workspaceExchangeTimeout preserves the existing control request budget across all connection attempts.
// workspaceExchangeTimeout сохраняет прежний бюджет управляющего запроса для всех попыток подключения вместе.
const workspaceExchangeTimeout = 35 * time.Second

// workspaceExchangeFailureMessage identifies safe local diagnostics; raw transport errors and responses are never logged.
// workspaceExchangeFailureMessage обозначает безопасную локальную диагностику без raw transport errors и ответов.
const workspaceExchangeFailureMessage = "runtime workspace owner exchange rejected"

// ObserveWorkspace sends an immutable observation and requires the exact closed owner ACK.
// Only a refused TCP dial may be retried; redirects and uncertain HTTP outcomes never permit replay or writes.
// ObserveWorkspace отправляет immutable observation и требует exact closed owner ACK.
// Повторяется только refused TCP dial; redirects и unknown HTTP outcome не разрешают replay или запись.
func (client *Client) ObserveWorkspace(ctx context.Context, input model.Input, observation runtimecontract.RunnerWorkspaceObservation) (runtimecontract.WorkspaceWriterAcknowledgement, error) {
	var empty runtimecontract.WorkspaceWriterAcknowledgement
	if observation.Phase != runtimecontract.WorkspaceObserveBind || observation.ValidateForInput(input) != nil {
		return empty, errors.New("runtime workspace observation is invalid")
	}
	raw, err := json.Marshal(observation)
	if err != nil || len(raw) > runtimecontract.MaximumWorkspaceObservationBytes {
		return empty, errors.New("runtime workspace observation is invalid")
	}
	defer clear(raw)
	endpoint := *client.base
	endpoint.Path = "/v1/executions/" + url.PathEscape(input.LeaseRef) + "/workspace"
	exchangeContext, cancel := context.WithTimeout(ctx, workspaceExchangeTimeout)
	defer cancel()
	response, err := client.workspaceExchange(exchangeContext, input, endpoint.String(), raw)
	if err != nil {
		return empty, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		slog.Warn(workspaceExchangeFailureMessage, "failure_class", "HTTP_REJECTED", "http_status", response.StatusCode)
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, runtimecontract.MaximumWorkspaceQuiescenceBytes+1))
		return empty, errors.New("runtime workspace owner rejected observation")
	}
	ackRaw, err := io.ReadAll(io.LimitReader(response.Body, runtimecontract.MaximumWorkspaceQuiescenceBytes+1))
	if err != nil {
		slog.Warn(workspaceExchangeFailureMessage, "failure_class", "ACK_READ")
		return empty, errors.New("read runtime workspace owner acknowledgement")
	}
	ack, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement(ackRaw)
	if err != nil || !ack.Matches(input) {
		slog.Warn(workspaceExchangeFailureMessage, "failure_class", "ACK_INVALID")
		return empty, errors.New("runtime workspace owner acknowledgement is invalid")
	}
	return ack, nil
}

// workspaceExchange retries only proven pre-connection refusals, retaining the same ticket, input and body within the caller's budget.
// It deliberately rejects redirects and does not retry TLS, HTTP, read, write or unknown transport outcomes.
// workspaceExchange повторяет только доказанные отказы до соединения с теми же ticket, input и body в пределах срока caller.
// Redirects отклоняются; TLS, HTTP, read, write и unknown transport outcome не повторяются.
func (client *Client) workspaceExchange(ctx context.Context, input model.Input, endpoint string, raw []byte) (*http.Response, error) {
	httpClient := *client.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for attempt := 0; ; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			return nil, errors.New("create runtime workspace observation")
		}
		request.Header.Set("Authorization", "Bearer "+client.token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		bindExecutionHeaders(request, input, "workspace")
		response, err := httpClient.Do(request)
		if err == nil {
			if attempt > 0 {
				slog.Info("runtime workspace owner connection recovered", "dial_attempts", attempt+1)
			}
			return response, nil
		}
		var operation *net.OpError
		refused := errors.As(err, &operation) && operation.Op == "dial" &&
			(operation.Net == "tcp" || operation.Net == "tcp4" || operation.Net == "tcp6") && errors.Is(err, syscall.ECONNREFUSED)
		if !refused || attempt == len(workspaceConnectDelays) || ctx.Err() != nil {
			failureClass := "TRANSPORT"
			if refused {
				failureClass = "DIAL_REFUSED"
			}
			slog.Warn(workspaceExchangeFailureMessage, "failure_class", failureClass, "dial_attempts", attempt+1)
			return nil, errors.New("runtime workspace owner is unavailable")
		}
		timer := time.NewTimer(workspaceConnectDelays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.New("runtime workspace owner is unavailable")
		case <-timer.C:
		}
	}
}

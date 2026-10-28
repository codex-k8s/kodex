package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// ObserveWorkspace sends one immutable observation and requires the exact closed owner ACK.
// Unknown transport outcomes do not permit writes; no body, credential or upstream error is exposed.
// ObserveWorkspace отправляет одно immutable observation и требует exact closed owner ACK.
// Unknown transport outcome не разрешает запись; body, credential и upstream error не раскрываются.
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
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return empty, errors.New("create runtime workspace observation")
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	bindExecutionHeaders(request, input, "workspace")
	response, err := client.http.Do(request)
	if err != nil {
		return empty, errors.New("runtime workspace owner is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, runtimecontract.MaximumWorkspaceQuiescenceBytes+1))
		return empty, errors.New("runtime workspace owner rejected observation")
	}
	ackRaw, err := io.ReadAll(io.LimitReader(response.Body, runtimecontract.MaximumWorkspaceQuiescenceBytes+1))
	if err != nil {
		return empty, errors.New("read runtime workspace owner acknowledgement")
	}
	ack, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement(ackRaw)
	if err != nil || !ack.Matches(input) {
		return empty, errors.New("runtime workspace owner acknowledgement is invalid")
	}
	return ack, nil
}

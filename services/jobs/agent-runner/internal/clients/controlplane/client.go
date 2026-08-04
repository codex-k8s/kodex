// Package controlplane реализует узкий client profile одного claimed turn.
package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
	"github.com/google/uuid"
)

type Client struct {
	shared *sharedclient.Client
	input  model.Input
}

type TurnLease struct {
	Version, Generation uint64
	Attempt             uint32
	Token               string
	ExpiresAt           time.Time
}

type Execution struct {
	Version, Fence, Generation uint64
	State                      controlplanev1.RuntimeExecutionState
}

func Dial(ctx context.Context, input model.Input) (*Client, error) {
	shared, err := sharedclient.Dial(ctx, sharedclient.Config{Target: input.ControlPlane.Target,
		TLSServerName: input.ControlPlane.TLS.ServerName, CAFile: input.ControlPlane.TLS.CAFile,
		ClientCertificateFile: input.ControlPlane.TLS.CertificateFile,
		ClientPrivateKeyFile:  input.ControlPlane.TLS.PrivateKeyFile,
		ApplicationGrantFile:  input.CredentialFiles.ControlPlaneGrant,
		ExpectedIssuerUID:     29001, ExpectedIssuerGID: 29000, DialTimeout: 2 * time.Second,
		Operations: sharedclient.AgentRunnerOperations()})
	if err != nil {
		return nil, err
	}
	return &Client{shared: shared, input: input}, nil
}

func (client *Client) Close() error                    { return client.shared.Close() }
func (client *Client) Check(ctx context.Context) error { return client.shared.Check(ctx) }

func (client *Client) Claim(ctx context.Context) (TurnLease, error) {
	response, err := client.shared.ControlPlane.ClaimTurn(ctx,
		&controlplanev1.ClaimTurnRequest{IdempotencyKey: uuid.NewSHA1(uuid.NameSpaceURL,
			[]byte("agent-runner:claim:"+client.input.ExecutionID)).String()})
	if err != nil {
		return TurnLease{}, err
	}
	turn, spec := response.GetTurn(), response.GetTurn().GetSpec().GetTurn()
	if turn == nil || spec == nil || turn.GetId() != client.input.TurnID ||
		spec.GetSessionId() != client.input.SessionID || spec.GetAttempt() != client.input.Attempt ||
		spec.GetEffectiveInputSha256() != client.input.ImmutableInputSHA256 ||
		spec.GetRuntimeRevisionId() != client.input.RuntimeRevisionID || response.GetLeaseToken() == "" ||
		response.GetAttempt() != client.input.Attempt || response.GetAuthorityGeneration() != client.input.GrantGeneration ||
		response.GetLeaseExpiresAt() == nil {
		return TurnLease{}, errors.New("claimed turn does not match runtime input")
	}
	return TurnLease{Version: turn.GetVersion(), Generation: response.GetAuthorityGeneration(),
		Attempt: response.GetAttempt(), Token: response.GetLeaseToken(), ExpiresAt: response.GetLeaseExpiresAt().AsTime()}, nil
}

func (client *Client) GetExecution(ctx context.Context) (Execution, error) {
	response, err := client.shared.ControlPlane.GetRuntimeExecution(ctx, &controlplanev1.GetRuntimeExecutionRequest{
		ExecutionId: client.input.ExecutionID, ExpectedVersion: client.input.ExecutionVersion})
	if err != nil {
		return Execution{}, err
	}
	execution := response.GetExecution()
	if execution == nil || execution.GetExecutionId() != client.input.ExecutionID ||
		execution.GetSessionId() != client.input.SessionID || execution.GetTurnId() != client.input.TurnID ||
		execution.GetAttempt() != client.input.Attempt || execution.GetRuntimeRevisionSha256() != client.input.RuntimeRevisionSHA256 ||
		execution.GetImmutableInputSha256() != client.input.ImmutableInputSHA256 ||
		execution.GetProviderBindingId() != client.input.ProviderBindingID ||
		execution.GetProviderBindingVersion() != client.input.ProviderBindingVersion ||
		execution.GetProviderBindingSha256() != client.input.ProviderBindingSHA256 ||
		execution.GetGrantGeneration() != client.input.GrantGeneration || execution.GetVersion() < client.input.ExecutionVersion ||
		execution.GetFence() < client.input.Fence {
		return Execution{}, errors.New("runtime execution readback is invalid")
	}
	return Execution{Version: execution.GetVersion(), Fence: execution.GetFence(),
		Generation: execution.GetGrantGeneration(), State: execution.GetState()}, nil
}

func (client *Client) Progress(ctx context.Context, lease TurnLease,
	kind controlplanev1.RuntimeProgressKind, sequence uint32, markdown string) error {
	execution, err := client.GetExecution(ctx)
	if err != nil {
		return err
	}
	response, err := client.shared.ControlPlane.ReportRuntimeProgress(ctx, &controlplanev1.ReportRuntimeProgressRequest{
		IdempotencyKey: uuid.NewSHA1(uuid.NameSpaceURL, []byte("agent-runner:progress:"+client.input.ExecutionID+":"+
			kind.String()+":"+fmt.Sprint(sequence))).String(), TurnId: client.input.TurnID,
		LeaseToken: lease.Token, ExpectedTurnVersion: lease.Version, Attempt: lease.Attempt,
		AuthorityGeneration: lease.Generation, ExecutionId: client.input.ExecutionID,
		ExpectedExecutionVersion: execution.Version, ExpectedExecutionFence: execution.Fence,
		Kind: kind, Sequence: sequence, Markdown: markdown})
	if err != nil {
		return err
	}
	if response.GetDeliveryId() == "" || response.GetExecution().GetExecutionId() != client.input.ExecutionID {
		return errors.New("runtime progress receipt is invalid")
	}
	return nil
}

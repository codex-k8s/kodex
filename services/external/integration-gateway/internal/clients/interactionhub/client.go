// Package interactionhub contains integration-gateway's interaction-hub client boundary.
package interactionhub

import (
	"context"
	"fmt"
	"strings"
	"time"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/clientruntime"
	"google.golang.org/grpc"
)

// Config contains interaction-hub gRPC connection settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// ObjectRef points to sanitized callback content or attachments outside the gateway.
type ObjectRef struct {
	URI       string
	Digest    string
	SizeBytes *int64
}

// CallbackEnvelope is the safe edge callback forwarded to interaction-hub.
type CallbackEnvelope struct {
	CallbackSource  string
	ContractVersion string
	CallbackID      string
	DeliveryID      string
	RequestRef      string
	ActorRef        string
	Action          string
	AnswerSummary   string
	AnswerObject    ObjectRef
	GatewayRef      string
	ReceivedAt      time.Time
	RequestID       string
	CorrelationID   string
	ClientIPHash    string
}

// CallbackResult contains the safe interaction-hub response used by HTTP handlers.
type CallbackResult struct {
	CallbackID string
}

// Client calls interaction-hub with platform service metadata.
type Client struct {
	client    interactionsv1.InteractionHubServiceClient
	authToken string
	timeout   time.Duration
}

// Disabled is used while the external callback route stays inactive.
type Disabled struct{}

// RecordChannelCallback reports that the callback route is not active in this process.
func (Disabled) RecordChannelCallback(context.Context, CallbackEnvelope) (CallbackResult, error) {
	return CallbackResult{}, ErrDisabled
}

// ErrDisabled is returned by the disabled interaction-hub client.
var ErrDisabled = fmt.Errorf("external callback route is disabled")

// NewConnection creates a gRPC client connection to interaction-hub.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return clientruntime.NewConnection(cfg.Addr, "interaction-hub")
}

// New wraps a generated interaction-hub client.
func New(client interactionsv1.InteractionHubServiceClient, cfg Config) (*Client, error) {
	authToken, timeout, err := clientruntime.ClientSettings(client == nil, "interaction-hub", cfg.AuthToken, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	return &Client{client: client, authToken: authToken, timeout: timeout}, nil
}

// RecordChannelCallback forwards the verified callback envelope to interaction-hub.
func (c *Client) RecordChannelCallback(ctx context.Context, callback CallbackEnvelope) (CallbackResult, error) {
	callCtx, cancel := context.WithTimeout(outgoingContext(ctx, c.authToken, callback), c.timeout)
	defer cancel()
	idempotencyKey := callbackIdempotencyKey(callback)
	request := &interactionsv1.RecordChannelCallbackRequest{
		Meta:     channelCallbackCommandMeta(callback, idempotencyKey),
		Callback: channelCallbackEnvelope(callback),
	}
	response, err := c.client.RecordChannelCallback(callCtx, request)
	if err != nil {
		return CallbackResult{}, err
	}
	result := CallbackResult{CallbackID: callback.CallbackID}
	if response.GetCallback() != nil {
		result.CallbackID = response.GetCallback().GetCallbackId()
	}
	return result, nil
}

func channelCallbackCommandMeta(callback CallbackEnvelope, idempotencyKey string) *interactionsv1.CommandMeta {
	return &interactionsv1.CommandMeta{
		IdempotencyKey: &idempotencyKey,
		Actor:          &interactionsv1.Actor{Type: "service", Id: clientruntime.CallerID},
		Reason:         "external channel callback edge ingress",
		RequestId:      strings.TrimSpace(callback.RequestID),
		RequestContext: &interactionsv1.RequestContext{
			ClientIpHash: clientruntime.OptionalString(callback.ClientIPHash),
			Source:       clientruntime.CallerID,
			TraceId:      clientruntime.OptionalString(callback.CorrelationID),
		},
	}
}

func callbackIdempotencyKey(callback CallbackEnvelope) string {
	source := strings.TrimSpace(callback.CallbackSource)
	callbackID := strings.TrimSpace(callback.CallbackID)
	if source == "" {
		return callbackID
	}
	return source + ":" + callbackID
}

func channelCallbackEnvelope(callback CallbackEnvelope) *interactionsv1.ChannelCallbackEnvelope {
	envelope := &interactionsv1.ChannelCallbackEnvelope{
		ContractVersion: callback.ContractVersion,
		CallbackId:      callback.CallbackID,
		Action:          callback.Action,
		SignatureStatus: interactionsv1.CallbackSignatureStatus_CALLBACK_SIGNATURE_STATUS_VERIFIED,
		ReceivedAt:      callback.ReceivedAt.UTC().Format(time.RFC3339Nano),
		CorrelationId:   callback.CorrelationID,
		DeliveryId:      clientruntime.OptionalString(callback.DeliveryID),
		RequestRef:      clientruntime.OptionalString(callback.RequestRef),
		ActorRef:        clientruntime.OptionalString(callback.ActorRef),
		AnswerSummary:   clientruntime.OptionalString(callback.AnswerSummary),
		GatewayRef:      clientruntime.OptionalString(callback.GatewayRef),
	}
	if objectRef := channelCallbackObjectRef(callback.AnswerObject); objectRef != nil {
		envelope.AnswerObject = objectRef
	}
	return envelope
}

func channelCallbackObjectRef(input ObjectRef) *interactionsv1.ObjectRef {
	uri := strings.TrimSpace(input.URI)
	digest := strings.TrimSpace(input.Digest)
	if uri == "" && digest == "" && input.SizeBytes == nil {
		return nil
	}
	return &interactionsv1.ObjectRef{
		ObjectUri:       uri,
		ObjectDigest:    digest,
		ObjectSizeBytes: input.SizeBytes,
	}
}

func outgoingContext(ctx context.Context, authToken string, callback CallbackEnvelope) context.Context {
	return clientruntime.OutgoingContext(ctx, clientruntime.RequestMetadata{
		AuthToken:     authToken,
		RequestID:     callback.RequestID,
		CorrelationID: callback.CorrelationID,
	})
}

package httptransport

import (
	"context"
	"time"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
)

type Config struct {
	ServiceName     string
	OpenAPISpecPath string
	RequestTimeout  time.Duration
	MaxBodyBytes    int64
}

type InteractionHubClient interface {
	ownerInboxReader
	ownerInboxResponder
}

type ownerInboxReader interface {
	ListOwnerInboxItems(context.Context, *interactionsv1.ListOwnerInboxItemsRequest) (*interactionsv1.ListOwnerInboxItemsResponse, error)
	GetOwnerInboxItem(context.Context, *interactionsv1.GetOwnerInboxItemRequest) (*interactionsv1.OwnerInboxItemResponse, error)
}

type ownerInboxResponder interface {
	RecordInteractionResponse(context.Context, *interactionsv1.RecordInteractionResponseRequest) (*interactionsv1.InteractionResponseResponse, error)
}

type AgentManagerClient interface {
	GetAgentRunRuntimeStatus(context.Context, *agentsv1.GetAgentRunRuntimeStatusRequest) (*agentsv1.AgentRunRuntimeStatusResponse, error)
}

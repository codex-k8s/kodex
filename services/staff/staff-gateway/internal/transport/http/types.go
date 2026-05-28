package httptransport

import (
	"context"
	"time"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
)

type Config struct {
	ServiceName     string
	OpenAPISpecPath string
	RequestTimeout  time.Duration
	MaxBodyBytes    int64
}

type InteractionHubClient interface {
	ListOwnerInboxItems(context.Context, *interactionsv1.ListOwnerInboxItemsRequest) (*interactionsv1.ListOwnerInboxItemsResponse, error)
	GetOwnerInboxItem(context.Context, *interactionsv1.GetOwnerInboxItemRequest) (*interactionsv1.OwnerInboxItemResponse, error)
	RecordInteractionResponse(context.Context, *interactionsv1.RecordInteractionResponseRequest) (*interactionsv1.InteractionResponseResponse, error)
}

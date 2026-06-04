package httptransport

import (
	"context"
	"time"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
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
	agentSessionReader
	agentRunSummaryReader
	agentRunRuntimeReader
	agentActivityReader
	selfDeployPlanReader
}

type agentSessionReader interface {
	ListAgentSessions(context.Context, *agentsv1.ListAgentSessionsRequest) (*agentsv1.ListAgentSessionsResponse, error)
}

type agentRunSummaryReader interface {
	ListAgentRunSummaries(context.Context, *agentsv1.ListAgentRunSummariesRequest) (*agentsv1.ListAgentRunSummariesResponse, error)
}

type agentRunRuntimeReader interface {
	GetAgentRunRuntimeStatus(context.Context, *agentsv1.GetAgentRunRuntimeStatusRequest) (*agentsv1.AgentRunRuntimeStatusResponse, error)
}

type agentActivityReader interface {
	ListAgentActivities(context.Context, *agentsv1.ListAgentActivitiesRequest) (*agentsv1.ListAgentActivitiesResponse, error)
}

type selfDeployPlanReader interface {
	ListSelfDeployPlans(context.Context, *agentsv1.ListSelfDeployPlansRequest) (*agentsv1.ListSelfDeployPlansResponse, error)
}

type GovernanceManagerClient interface {
	GetGovernanceSummary(context.Context, *governancev1.GetGovernanceSummaryRequest) (*governancev1.GovernanceSummaryResponse, error)
	SubmitGateDecision(context.Context, *governancev1.SubmitGateDecisionRequest) (*governancev1.GateDecisionResponse, error)
}

type ProjectCatalogClient interface {
	GetSelfDeploySignal(context.Context, *projectsv1.GetSelfDeploySignalRequest) (*projectsv1.SelfDeploySignalResponse, error)
	ListRepositories(context.Context, *projectsv1.ListRepositoriesRequest) (*projectsv1.ListRepositoriesResponse, error)
}

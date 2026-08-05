package botservice

import "context"

type BindingRequest struct {
	ControlSessionID, ChannelID, RootPostID, BotStableKey string
	ExecutionID, TurnID                                   string
	Attempt                                               uint32
}

type Binding struct {
	AgentSessionKey, AgentSessionBindingSHA256                string
	AgentSessionID                                            int64
	AgentSessionVersion                                       uint64
	ImmutableSecretRef, ProviderContentVersion, ContentSHA256 string
	ExecutionID, TurnID                                       string
	Attempt                                                   uint32
}

type Client interface {
	EnsureRuntimeMCPBinding(context.Context, BindingRequest) (Binding, error)
}

package botservice

import "context"

type BindingRequest struct {
	ControlSessionID, ChannelID, RootPostID, BotStableKey string
}

type Binding struct {
	AgentSessionKey, AgentSessionBindingSHA256                    string
	AgentSessionID                                                int64
	AgentSessionVersion                                           uint64
	ImmutableSecretRef, ProviderContentVersion, ContentSHA256     string
}

type Client interface {
	EnsureRuntimeMCPBinding(context.Context, BindingRequest) (Binding, error)
}

package mail

import (
	"context"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

type Authority interface {
	Resolve(context.Context, api.AuthorizationRequest) (api.AuthorizationDecision, error)
}
type Provider interface {
	Read(context.Context, api.Mailbox, api.Command) (api.Result, error)
	Send(context.Context, api.Mailbox, api.Command, string) (string, error)
	Delete(context.Context, api.Mailbox, string) (string, error)
	Apply(context.Context, api.Mailbox, api.Command, string) (api.Result, error)
	Ready(context.Context, api.Mailbox) error
	Probe(context.Context, api.Mailbox) api.Result
}

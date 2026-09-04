package receipt

import (
	"context"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

type Scope struct{ Tenant, Mailbox string }
type Record struct{ ID, Key, Digest, Status string }
type Repository interface {
	Reserve(context.Context, Scope, string, string, string) (Record, bool, error)
	Complete(context.Context, Scope, Record, string) error
	Get(context.Context, Scope, string, string) (Record, error)
	Configuration(context.Context, api.Configuration, string) error
	Ready(context.Context) error
}

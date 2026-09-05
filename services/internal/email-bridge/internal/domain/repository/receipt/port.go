package receipt

import (
	"context"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

type Scope struct{ Tenant, Mailbox string }
type Audit struct {
	Actor, Agent, Grant                         string
	Operation                                   api.Operation
	ConfigurationRevision, CredentialGeneration int64
	GateApproved                                bool
}

func (a Audit) Valid() bool {
	return a.Actor != "" && a.Agent != "" && a.Grant != "" && a.Operation.Valid() && a.ConfigurationRevision > 0 && a.CredentialGeneration > 0
}

type Record struct {
	Audit                                                         Audit
	ID, Key, Digest, Status, Resource, UID, Folder, ContentDigest string
	UIDValidity                                                   uint32
	ReportVersion                                                 int64
}

func (r Record) Result() api.Result {
	return api.Result{Status: r.Status, MessageId: r.ID, Uid: r.UID, UidValidity: r.UIDValidity, Folder: r.Folder, ContentDigest: r.ContentDigest}
}

type Repository interface {
	Reserve(context.Context, Scope, string, string, string, string, Audit) (Record, bool, error)
	Complete(context.Context, Scope, Record, string) error
	Get(context.Context, Scope, string, string) (Record, error)
	Configuration(context.Context, api.Configuration, string) error
	Ready(context.Context) error
}

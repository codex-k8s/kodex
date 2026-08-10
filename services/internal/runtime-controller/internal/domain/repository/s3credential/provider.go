// Package s3credential задаёт узкий порт выдачи execution-scoped S3 credentials.
package s3credential

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
)

type Action string

const (
	ActionArchive Action = "archive"
	ActionRestore Action = "restore"
)

func (action Action) Valid() bool { return action == ActionArchive || action == ActionRestore }

type Request struct {
	Execution         entity.Execution
	Action            Action
	SourceExecutionID string
	PolicyRaw         []byte
}

type Issue struct {
	AccessKeyID, SecretAccessKey, SessionToken string
	ExpiresAt                                  time.Time
	BootstrapLeaseID, LoginAccessor            string
	AssumedRoleARN, SessionName                string
	RevocationToken                            string
}

type Provider interface {
	Issue(context.Context, Request) (Issue, error)
	Check(context.Context, Request) error
	Ready(context.Context, Action) error
	Revoke(context.Context, Request, Issue) error
	Close()
}

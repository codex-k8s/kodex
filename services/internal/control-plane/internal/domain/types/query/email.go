package query

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

type EmailAuthorization struct {
	Binding                                               entity.EmailExecutionBinding
	MailboxRef, Operation, SemanticInputDigest, EffectKey string
	Sender, Folder, DestinationFolder                     string
	ConfigurationRevision                                 int64
}

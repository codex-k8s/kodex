package query

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

// EmailAuthorization carries the resolved execution binding and exact mailbox,
// operation and effect pins for an owner-authorized email read or mutation.
// EmailAuthorization переносит разрешённую связь исполнения и точные привязки
// ящика, операции и эффекта для санкционированной владельцем почтовой операции.
type EmailAuthorization struct {
	Binding                                               entity.EmailExecutionBinding
	MailboxRef, Operation, SemanticInputDigest, EffectKey string
	Sender, Folder, DestinationFolder                     string
	ConfigurationRevision                                 int64
}

package emailpolicy

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// AuthorizeCredentialOwner checks only the mailbox credential ownership boundary
// against a fresh, server-resolved root initiator and the expected generation.
// Personal mail requires that same active user; shared mail requires an explicitly
// configured active service subject. A shared identity does not replace current
// Connection, project, tool, mailbox-policy or effect-approval authorization.
// AuthorizeCredentialOwner проверяет только владение credentials ящика по свежему
// инициатору корня, разрешённому сервером, и ожидаемому поколению. Личная почта
// требует того же активного пользователя; общая — явно настроенного активного
// служебного субъекта. Общая identity не заменяет текущие проверки подключения,
// проекта, инструмента, политики ящика и подтверждения эффекта.
func AuthorizeCredentialOwner(owner entity.EmailMailboxCredentialOwner, initiator entity.IntegrationCredentialInitiator, expectedGeneration int64) error {
	if owner.OrganizationID == "" || owner.SubjectID == "" || owner.Generation < 1 {
		return errs.ErrUnavailable
	}
	switch owner.Policy {
	case entity.IntegrationCredentialInitiatorRequired:
		if owner.SubjectKind != "USER" {
			return errs.ErrUnavailable
		}
	case entity.IntegrationCredentialProjectShared:
		if owner.SubjectKind != "SERVICE" {
			return errs.ErrUnavailable
		}
	default:
		return errs.ErrUnavailable
	}
	if expectedGeneration < 1 || owner.Generation != expectedGeneration || !owner.SubjectActive ||
		initiator.OrganizationID != owner.OrganizationID || initiator.SubjectID == "" ||
		(initiator.SubjectKind != "USER" && initiator.SubjectKind != "SERVICE") {
		return errs.ErrForbidden
	}
	if owner.Policy == entity.IntegrationCredentialInitiatorRequired &&
		(initiator.SubjectKind != "USER" || initiator.SubjectID != owner.SubjectID) {
		return errs.ErrForbidden
	}
	if initiator.SubjectKind == "SERVICE" && initiator.SubjectID != owner.SubjectID {
		return errs.ErrForbidden
	}
	return nil
}

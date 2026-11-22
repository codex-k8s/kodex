package emailpolicy

import (
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// MinimumApprovalPolicy returns the closed operation-specific floor selected for personal email; draft deletion belongs to the draft scope, while message deletion requires each-effect approval.
// MinimumApprovalPolicy возвращает выбранный закрытый минимум подтверждения личной почты; удаление черновика относится к его области, удаление письма требует подтверждения каждого эффекта.
func MinimumApprovalPolicy(operation string) (integrationpackage.ApprovalPolicy, error) {
	switch operation {
	case "email.delivery.health.read", "email.mailbox.list", "email.message.list", "email.message.search", "email.message.read",
		"email.attachment.read", "email.message.status.read", "email.thread.read", "email.attachment.list":
		return integrationpackage.ApprovalNone, nil
	case "email.message.mark_read", "email.message.mark_unread", "email.message.move", "email.message.archive",
		"email.draft.create", "email.draft.update", "email.draft.delete":
		return integrationpackage.ApprovalHumanScoped, nil
	case "email.message.send", "email.message.reply", "email.message.reply_all", "email.message.forward", "email.message.delete":
		return integrationpackage.ApprovalHumanEachEffect, nil
	default:
		return "", errs.ErrForbidden
	}
}

// ValidateCommandApprovalPolicy retains the mailbox deny/each-effect restriction in addition to the package floor; reusable consent cannot satisfy a stricter mailbox human gate.
// ValidateCommandApprovalPolicy сохраняет запрет либо each-effect ограничение mailbox сверх минимума package; переиспользуемое согласие не заменяет более строгий human gate ящика.
func ValidateCommandApprovalPolicy(mailbox MailboxProjection, operation string, boundedInput []byte, selected string) error {
	minimum, err := MinimumApprovalPolicy(operation)
	if err != nil {
		return err
	}
	requiresGate, err := CommandRequiresGate(mailbox, operation, "", boundedInput)
	if err != nil {
		return err
	}
	if err := ValidateCommandResourceBounds(mailbox, operation, boundedInput); err != nil {
		return err
	}
	if requiresGate && minimum != integrationpackage.ApprovalNone {
		minimum = integrationpackage.ApprovalHumanEachEffect
	}
	switch minimum {
	case integrationpackage.ApprovalNone:
		if selected != string(integrationpackage.ApprovalNone) && selected != string(integrationpackage.ApprovalHumanEachEffect) {
			return errs.ErrForbidden
		}
	case integrationpackage.ApprovalHumanScoped:
		if selected != string(integrationpackage.ApprovalHumanScoped) && selected != string(integrationpackage.ApprovalHumanEachEffect) {
			return errs.ErrForbidden
		}
	case integrationpackage.ApprovalHumanEachEffect:
		if selected != string(integrationpackage.ApprovalHumanEachEffect) {
			return errs.ErrForbidden
		}
	default:
		return errs.ErrForbidden
	}
	return nil
}

// ScopedCommandFolders returns the exact command folders after applying authoritative mailbox defaults, preventing omitted folder input from producing an unbound reusable scope.
// ScopedCommandFolders возвращает точные папки команды после применения defaults авторитетного mailbox, чтобы отсутствие folder во вводе не оставляло переиспользуемую область непривязанной.
func ScopedCommandFolders(mailbox MailboxProjection, operation string, boundedInput []byte) (string, string, error) {
	command, err := api.CommandForIntegration(operation, mailbox.Ref, mailbox.Sender, "", boundedInput)
	if err != nil {
		return "", "", errs.ErrInvalid
	}
	folder := command.Folder
	if folder == "" {
		folder = mailbox.Folder
		if command.Operation == api.OperationDraftCreate || command.Operation == api.OperationDraftUpdate || command.Operation == api.OperationDraftDelete {
			folder = mailbox.DraftsFolder
		}
	}
	destination := command.DestinationFolder
	if command.Operation == api.OperationArchive {
		if destination != "" && destination != mailbox.ArchiveFolder {
			return "", "", errs.ErrForbidden
		}
		destination = mailbox.ArchiveFolder
	}
	return folder, destination, nil
}

// ValidateCommandResourceBounds checks current folders, destinations and every explicit recipient before proposing approval; passing the pure validation gate flag creates no approval, token, reservation or effect.
// ValidateCommandResourceBounds проверяет текущие папки, назначения и каждого явного получателя до предложения подтверждения; gate flag чистой проверки не создаёт approval, token, reservation либо effect.
func ValidateCommandResourceBounds(mailbox MailboxProjection, operation string, boundedInput []byte) error {
	command, err := api.CommandForIntegration(operation, mailbox.Ref, mailbox.Sender, "", boundedInput)
	if err != nil {
		return errs.ErrInvalid
	}
	folder, destination, err := ScopedCommandFolders(mailbox, operation, boundedInput)
	if err != nil {
		return err
	}
	input := query.EmailAuthorization{MailboxRef: mailbox.Ref, Sender: mailbox.Sender, ConfigurationRevision: mailbox.Revision,
		Operation: string(command.Operation), EffectKey: command.EffectKey, SemanticInputDigest: api.Digest(command), Folder: folder, DestinationFolder: destination}
	_, _, err = AuthorizeCommand(mailbox, operation, "", boundedInput, input, true)
	return err
}

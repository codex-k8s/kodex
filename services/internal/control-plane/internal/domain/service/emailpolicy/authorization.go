package emailpolicy

import (
	"slices"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func CommandRequiresGate(mailbox MailboxProjection, operation, effectKey string, boundedInput []byte) (bool, error) {
	command, err := api.CommandForIntegration(operation, mailbox.Ref, mailbox.Sender, effectKey, boundedInput)
	if err != nil {
		return false, errs.ErrInvalid
	}
	for _, policy := range mailbox.Policies {
		if policy.Operation == command.Operation {
			if policy.Policy == api.Deny {
				return false, errs.ErrForbidden
			}
			return policy.Policy == api.HumanGate, nil
		}
	}
	return false, errs.ErrForbidden
}

func ValidateExecutionBinding(binding entity.EmailExecutionBinding, now time.Time, allowExpired bool) error {
	if (binding.InvocationRef == "") == (binding.ConnectionTestRef == "") || binding.LeaseRef == "" ||
		binding.Fence == "" || binding.Generation < 1 || binding.ExpiresAt.IsZero() ||
		(!allowExpired && !binding.ExpiresAt.After(now)) {
		return errs.ErrInvalid
	}
	return nil
}

// AuthorizeCommand сверяет semantic digest через общий mapping, а scope сужает
// до exact command после проверки mailbox policy. Caller не задаёт recipients.
func AuthorizeCommand(mailbox MailboxProjection, operation, effectKey string, boundedInput []byte, input query.EmailAuthorization, gateApproved bool) (entity.EmailAuthorizationScope, string, error) {
	command, err := api.CommandForIntegration(operation, mailbox.Ref, mailbox.Sender, effectKey, boundedInput)
	if err != nil {
		return entity.EmailAuthorizationScope{}, "", errs.ErrInvalid
	}
	if string(command.Operation) != input.Operation || api.Digest(command) != input.SemanticInputDigest ||
		command.EffectKey != input.EffectKey || input.MailboxRef != mailbox.Ref || input.Sender != mailbox.Sender ||
		input.ConfigurationRevision != mailbox.Revision || !mailbox.Enabled {
		return entity.EmailAuthorizationScope{}, "", errs.ErrForbidden
	}
	folder := command.Folder
	if folder == "" {
		folder = mailbox.Folder
	}
	switch command.Operation {
	case api.OperationDraftCreate, api.OperationDraftUpdate, api.OperationDraftDelete:
		if command.Folder == "" {
			folder = mailbox.DraftsFolder
		}
		if mailbox.DraftsFolder == "" || folder != mailbox.DraftsFolder {
			return entity.EmailAuthorizationScope{}, "", errs.ErrForbidden
		}
	}
	destination := command.DestinationFolder
	if command.Operation == api.OperationArchive {
		destination = mailbox.ArchiveFolder
	}
	if input.Folder != folder || input.DestinationFolder != destination || !slices.Contains(mailbox.AllowedFolders, folder) ||
		(destination != "" && !slices.Contains(mailbox.AllowedFolders, destination)) {
		return entity.EmailAuthorizationScope{}, "", errs.ErrForbidden
	}
	if command.Operation == api.OperationMove || command.Operation == api.OperationArchive {
		if destination == "" || destination == folder {
			return entity.EmailAuthorizationScope{}, "", errs.ErrInvalid
		}
	}
	policy := api.Deny
	for _, candidate := range mailbox.Policies {
		if candidate.Operation != command.Operation {
			continue
		}
		policy = candidate.Policy
		if command.Operation != api.OperationMailboxes && len(candidate.Folders) != 0 &&
			(!slices.Contains(candidate.Folders, folder) || destination != "" && !slices.Contains(candidate.Folders, destination)) {
			return entity.EmailAuthorizationScope{}, "", errs.ErrForbidden
		}
	}
	if policy == api.Deny || policy == api.HumanGate && !gateApproved {
		return entity.EmailAuthorizationScope{}, "", errs.ErrForbidden
	}
	recipients := []string{}
	if command.Message.To != "" {
		recipients = append(recipients, command.Message.To)
	}
	recipients = append(recipients, command.Message.Cc...)
	recipients = append(recipients, command.Message.Bcc...)
	for _, recipient := range recipients {
		if !slices.Contains(mailbox.Recipients, recipient) {
			return entity.EmailAuthorizationScope{}, "", errs.ErrForbidden
		}
	}
	folders := []string{folder}
	if destination != "" && destination != folder {
		folders = append(folders, destination)
	}
	return entity.EmailAuthorizationScope{MailboxRef: mailbox.Ref, Sender: mailbox.Sender,
		Operations: []string{input.Operation}, Folders: folders, Recipients: recipients}, string(policy), nil
}

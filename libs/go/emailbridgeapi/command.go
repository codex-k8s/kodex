package emailbridgeapi

import "errors"

// CommandForIntegration — единый mapping для consumer и owner input digest.
// Строковые массивы соответствуют primitive-field контракту IntegrationPackage.
func CommandForIntegration(operation, mailbox, sender, effect string, raw []byte) (Command, error) {
	ops := map[string]Operation{"email.delivery.health.read": OperationHealth, "email.mailbox.list": OperationMailboxes, "email.message.list": OperationList, "email.message.search": OperationSearch, "email.message.read": OperationFetch, "email.attachment.read": OperationDownload, "email.message.send": OperationSend, "email.message.reply": OperationReply, "email.message.reply_all": OperationReplyAll, "email.message.forward": OperationForward, "email.message.delete": OperationDelete, "email.message.status.read": OperationReceipt, "email.thread.read": OperationThread, "email.attachment.list": OperationAttachments, "email.message.mark_read": OperationMarkRead, "email.message.mark_unread": OperationMarkUnread, "email.message.move": OperationMove, "email.message.archive": OperationArchive, "email.draft.create": OperationDraftCreate, "email.draft.update": OperationDraftUpdate, "email.draft.delete": OperationDraftDelete}
	op, ok := ops[operation]
	if !ok {
		return Command{}, errors.New("unknown email operation")
	}
	var input IntegrationInput
	if Decode(raw, &input) != nil {
		return Command{}, errors.New("invalid email integration input")
	}
	command := Command{Operation: op, MailboxId: mailbox, Uid: input.Uid, Cursor: input.Cursor, Query: input.Query, AttachmentIndex: input.AttachmentIndex, ReceiptId: input.MessageId, Folder: input.Folder, DestinationFolder: input.DestinationFolder, UidValidity: input.UidValidity, ThreadId: input.ThreadId, ExpectedDigest: input.ExpectedDigest}
	if op == OperationReceipt {
		if (input.MessageId == "") == (input.EffectKey == "") {
			return Command{}, errors.New("exactly one email receipt identifier required")
		}
		command.EffectKey = input.EffectKey
	}
	if IsMutation(op) {
		command.EffectKey = effect
	}
	if op == OperationSend || op == OperationReply || op == OperationReplyAll || op == OperationForward || op == OperationDraftCreate || op == OperationDraftUpdate {
		command.Message = MessageInput{From: sender, To: input.To, Subject: input.Subject, BodyText: input.BodyText, SourceUid: input.SourceUid, SourceUidValidity: input.SourceUidValidity}
		for _, field := range []struct {
			raw    string
			target *[]string
		}{{input.Cc, &command.Message.Cc}, {input.Bcc, &command.Message.Bcc}} {
			if field.raw == "" {
				continue
			}
			var recipients Recipients
			if Decode([]byte(field.raw), &recipients) != nil {
				return Command{}, errors.New("invalid email recipients")
			}
			for _, r := range recipients {
				if !Address(r) {
					return Command{}, errors.New("invalid email recipient")
				}
			}
			*field.target = recipients
		}
		if input.Attachments != "" {
			var attachments Attachments
			if Decode([]byte(input.Attachments), &attachments) != nil {
				return Command{}, errors.New("invalid email attachments")
			}
			command.Message.Attachments = attachments
		}
	}
	return command, nil
}

func IsMutation(op Operation) bool {
	switch op {
	case OperationSend, OperationReply, OperationReplyAll, OperationForward, OperationDelete, OperationMarkRead, OperationMarkUnread, OperationMove, OperationArchive, OperationDraftCreate, OperationDraftUpdate, OperationDraftDelete:
		return true
	default:
		return false
	}
}

package mail

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"mime"
	"slices"
	"strings"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
)

type Service struct {
	Config    api.Configuration
	Authority Authority
	Provider  Provider
	Receipts  receipt.Repository
}

func Mutation(op api.Operation) bool {
	return op == api.OperationSend || op == api.OperationReply || op == api.OperationReplyAll || op == api.OperationForward || op == api.OperationDelete
}
func Sending(op api.Operation) bool { return Mutation(op) && op != api.OperationDelete }

func (s *Service) Execute(ctx context.Context, caller, token string, command api.Command) (api.Result, error) {
	if !command.Operation.Valid() || len(token) < 1 || len(token) > 16384 || len(command.EffectKey) > 128 || len(command.Query) > 256 || len(command.Cursor) > 512 || len(command.Uid) > 70 || strings.ContainsAny(command.Uid, "\r\n\x00") {
		return api.Result{}, errs.Invalid
	}
	var mailbox api.Mailbox
	for _, m := range s.Config.Mailboxes {
		if m.Id == command.MailboxId {
			mailbox = m
			break
		}
	}
	if mailbox.Id == "" || !mailbox.Enabled {
		return api.Result{}, errs.NotFound
	}
	if command.Folder != "" && command.Folder != "INBOX" {
		return api.Result{}, errs.Unsupported
	}
	if Mutation(command.Operation) && command.EffectKey == "" {
		return api.Result{}, errs.Invalid
	}
	if command.Operation==api.OperationReceipt&&((command.ReceiptId=="")== (command.EffectKey=="")){return api.Result{},errs.Invalid}
	if (command.Operation==api.OperationDelete||command.Operation==api.OperationFetch||command.Operation==api.OperationDownload)&&command.Uid==""{return api.Result{},errs.Invalid}
	if Sending(command.Operation) {
		if err := validateMessage(mailbox, command); err != nil {
			return api.Result{}, err
		}
	}
	request := api.AuthorizationRequest{InvocationToken: token, CallerSpiffeId: caller, MailboxId: mailbox.Id, Sender: mailbox.Sender, Operation: command.Operation, InputSha256: api.Digest(command), EffectKey: command.EffectKey, ConfigurationRevision: mailbox.Revision}
	decision, err := s.Authority.Resolve(ctx, request)
	if err != nil {
		return api.Result{}, err
	}
	if err = authorize(mailbox, command, request, decision); err != nil {
		return api.Result{}, err
	}
	if err = s.Receipts.Configuration(ctx, s.Config, api.Digest(s.Config)); err != nil {
		return api.Result{}, err
	}
	deadline:=time.Now().Add(time.Duration(mailbox.Limits.TimeoutSeconds)*time.Second)
	if expires:=time.Unix(decision.ExpiresAt,0);expires.Before(deadline){deadline=expires}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	scope := receipt.Scope{Tenant: mailbox.TenantId, Mailbox: mailbox.Id}
	if command.Operation == api.OperationMark {
		return api.Result{}, errs.Unsupported
	}
	if command.Operation == api.OperationReceipt {
		r, e := s.Receipts.Get(ctx, scope, command.ReceiptId, command.EffectKey)
		return api.Result{Status: r.Status, MessageId: r.ID}, e
	}
	if command.Operation == api.OperationMailboxes {
		return api.Result{Status: "ok", Mailboxes: []string{mailbox.Id}}, nil
	}
	if command.Operation == api.OperationHealth {
		if err := s.Receipts.Ready(ctx); err != nil {
			return api.Result{}, err
		}
		if err := s.Provider.Ready(ctx, mailbox); err != nil {
			return api.Result{}, err
		}
		return api.Result{Status: "ready"}, nil
	}
	if !Mutation(command.Operation) {
		return s.Provider.Read(ctx, mailbox, command)
	}
	id := fmt.Sprintf("%x", randomID())
	r, created, err := s.Receipts.Reserve(ctx, scope, command.EffectKey, request.InputSha256, id)
	if err != nil {
		return api.Result{}, err
	}
	if !created {
		return api.Result{Status: r.Status, MessageId: r.ID}, nil
	}
	status := "unknown"
	if Sending(command.Operation) {
		status, err = s.Provider.Send(ctx, mailbox, command, r.ID)
	} else {
		status, err = s.Provider.Delete(ctx, mailbox, command.Uid)
	}
	// При отмене/сбое запись unknown уже сохранена; завершение не повторяет протокол.
	if err != nil {
		return api.Result{Status: "unknown", MessageId: r.ID}, nil
	}
	if err = s.Receipts.Complete(ctx, scope, r, status); err != nil {
		return api.Result{Status: "unknown", MessageId: r.ID}, nil
	}
	return api.Result{Status: status, MessageId: r.ID}, nil
}

func randomID() []byte {
	v := make([]byte, 16)
	if _, e := rand.Read(v); e != nil {
		panic("random source unavailable")
	}
	return v
}
func authorize(m api.Mailbox, c api.Command, r api.AuthorizationRequest, d api.AuthorizationDecision) error {
	if !d.Allowed || d.ActorId == "" || d.AgentId == "" || d.GrantId == "" || d.TenantId != m.TenantId || d.ConnectionId != m.ConnectionId || d.MailboxId != m.Id || d.Operation != c.Operation || d.InputSha256 != r.InputSha256 || d.EffectKey != c.EffectKey || d.ConfigurationRevision != m.Revision || d.CredentialGeneration != m.Smtp.Password.Generation || d.ExpiresAt <= time.Now().Unix() || d.ExpiresAt > time.Now().Add(2*time.Minute).Unix() {
		return errs.Denied
	}
	policy := api.Deny
	for _, p := range m.Policies {
		if p.Operation == c.Operation {
			policy = p.Policy
		}
	}
	if !d.Policy.Valid() || policy == api.Deny || d.Policy == api.Deny {
		return errs.Denied
	}
	for _, scope := range []api.Scope{d.UserScope, d.AgentScope, d.ConnectionScope, d.ResourceScope} {
		if scope.MailboxId != m.Id || scope.Sender != m.Sender || !slices.Contains(scope.Operations, c.Operation) {
			return errs.Denied
		}
		if Sending(c.Operation) {
			for _, recipient := range recipients(c.Message) {
				if !slices.Contains(scope.Recipients, recipient) {
					return errs.Denied
				}
			}
		}
	}
	if (policy == api.HumanGate || d.Policy == api.HumanGate) && !d.GateApproved {
		return errs.Gate
	}
	return nil
}

func recipients(m api.MessageInput) []string {
	out := []string{m.To}
	out = append(out, m.Cc...)
	return append(out, m.Bcc...)
}
func validateMessage(m api.Mailbox, c api.Command) error {
	v := c.Message
	if v.From != m.Sender || len(v.Subject) > 998 || strings.ContainsAny(v.Subject, "\r\n\x00") || len(v.BodyText) > m.Limits.MessageBytes || len(v.Attachments) > m.Limits.MaxAttachments || len(recipients(v)) > m.Limits.MaxRecipients {
		return errs.Invalid
	}
	for _, r := range recipients(v) {
		if !api.Address(r) || !slices.Contains(m.Recipients, r) {
			return errs.Denied
		}
	}
	if c.Operation != api.OperationSend && (v.SourceUid == "" || len(v.SourceUid) > 70) {
		return errs.Invalid
	}
	total := len(v.BodyText)
	for _, a := range v.Attachments {
		if a.Filename == "" || len(a.Filename) > 255 || strings.ContainsAny(a.Filename, "/\\\r\n\x00") {
			return errs.Invalid
		}
		if _, _, err := mime.ParseMediaType(a.ContentType); err != nil {
			return errs.Invalid
		}
		b, err := base64.StdEncoding.DecodeString(a.ContentBase64)
		if err != nil || len(b) > m.Limits.AttachmentBytes {
			return errs.Invalid
		}
		total += len(b)
	}
	if total > m.Limits.MessageBytes {
		return errs.Invalid
	}
	return nil
}

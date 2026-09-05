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
	Ledger         receipt.ReconciliationRepository
	Reports        receipt.ReportRepository
	Effects        receipt.EffectAuthority
	CompletionBase context.Context
	Config         api.Configuration
	Authority      Authority
	Provider       Provider
	Receipts       receipt.Repository
}

const receiptCompletionTimeout = receipt.ReportGrace

func Mutation(op api.Operation) bool {
	switch op {
	case api.OperationSend, api.OperationReply, api.OperationReplyAll, api.OperationForward, api.OperationDelete, api.OperationMarkRead, api.OperationMarkUnread, api.OperationMove, api.OperationArchive, api.OperationDraftCreate, api.OperationDraftUpdate, api.OperationDraftDelete:
		return true
	default:
		return false
	}
}
func Sending(op api.Operation) bool {
	return op == api.OperationSend || op == api.OperationReply || op == api.OperationReplyAll || op == api.OperationForward
}

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
	folder := command.Folder
	if folder == "" {
		folder = mailbox.Folder
	}
	if command.Operation == api.OperationDraftCreate || command.Operation == api.OperationDraftUpdate || command.Operation == api.OperationDraftDelete {
		if mailbox.DraftsFolder == "" {
			return api.Result{}, errs.Unsupported
		}
		if command.Folder == "" {
			folder = mailbox.DraftsFolder
		}
		if folder != mailbox.DraftsFolder {
			return api.Result{}, errs.Denied
		}
	}
	destination := command.DestinationFolder
	if command.Operation == api.OperationArchive {
		destination = mailbox.ArchiveFolder
	}
	if command.Operation == api.OperationMove || command.Operation == api.OperationArchive {
		if destination == "" || destination == folder {
			return api.Result{}, errs.Invalid
		}
	}
	if !slices.Contains(mailbox.AllowedFolders, folder) {
		return api.Result{}, errs.Denied
	}
	if destination != "" && !slices.Contains(mailbox.AllowedFolders, destination) {
		return api.Result{}, errs.Denied
	}
	if mailbox.ReceiveProtocol == "pop3" {
		switch command.Operation {
		case api.OperationThread, api.OperationMarkRead, api.OperationMarkUnread, api.OperationMove, api.OperationArchive, api.OperationDraftCreate, api.OperationDraftUpdate, api.OperationDraftDelete:
			return api.Result{}, errs.Unsupported
		}
	}
	if Mutation(command.Operation) && command.EffectKey == "" {
		return api.Result{}, errs.Invalid
	}
	if command.Operation == api.OperationReceipt && ((command.ReceiptId == "") == (command.EffectKey == "")) {
		return api.Result{}, errs.Invalid
	}
	if (command.Operation == api.OperationDelete || command.Operation == api.OperationFetch || command.Operation == api.OperationDownload) && command.Uid == "" {
		return api.Result{}, errs.Invalid
	}
	if Sending(command.Operation) {
		if err := validateMessage(mailbox, command); err != nil {
			return api.Result{}, err
		}
	}
	if command.Operation == api.OperationDraftCreate || command.Operation == api.OperationDraftUpdate {
		if err := validateMessage(mailbox, command); err != nil {
			return api.Result{}, err
		}
	}
	if mailbox.ReceiveProtocol == "imap" {
		switch command.Operation {
		case api.OperationFetch, api.OperationDownload, api.OperationAttachments, api.OperationDelete, api.OperationMarkRead, api.OperationMarkUnread, api.OperationMove, api.OperationArchive, api.OperationDraftUpdate, api.OperationDraftDelete:
			if command.Uid == "" || command.UidValidity == 0 {
				return api.Result{}, errs.Invalid
			}
		}
		if command.Operation == api.OperationDraftUpdate && len(command.ExpectedDigest) != 64 {
			return api.Result{}, errs.Invalid
		}
		if Sending(command.Operation) && command.Operation != api.OperationSend && command.Message.SourceUidValidity == 0 {
			return api.Result{}, errs.Invalid
		}
	}
	request := api.AuthorizationRequest{InvocationToken: token, CallerSpiffeId: caller, MailboxId: mailbox.Id, Sender: mailbox.Sender, Operation: command.Operation, InputSha256: api.Digest(command), EffectKey: command.EffectKey, ConfigurationRevision: mailbox.Revision, Folder: folder, DestinationFolder: destination}
	request.ExecutionBinding = api.ExecutionFromContext(ctx)
	decision, err := s.Authority.Resolve(ctx, request)
	if err != nil {
		return api.Result{}, err
	}
	if err = authorize(mailbox, command, request, decision); err != nil {
		return api.Result{}, err
	}
	command.Folder = folder
	command.DestinationFolder = destination
	if err = s.Receipts.Configuration(ctx, s.Config, api.Digest(s.Config)); err != nil {
		return api.Result{}, err
	}
	deadline := time.Now().Add(time.Duration(mailbox.Limits.TimeoutSeconds) * time.Second)
	if expires := time.Unix(decision.ExpiresAt, 0); expires.Before(deadline) {
		deadline = expires
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	scope := receipt.Scope{Tenant: mailbox.TenantId, Mailbox: mailbox.Id}
	if command.Operation == api.OperationMark {
		return api.Result{}, errs.Unsupported
	}
	if command.Operation == api.OperationReceipt {
		r, e := s.Receipts.Get(ctx, scope, command.ReceiptId, command.EffectKey)
		return r.Result(), e
	}
	if command.Operation == api.OperationMailboxes {
		if mailbox.ReceiveProtocol == "imap" {
			allowed := []string{}
			for _, f := range mailbox.AllowedFolders {
				visible := true
				for _, scope := range []api.Scope{decision.UserScope, decision.AgentScope, decision.ConnectionScope, decision.ResourceScope} {
					visible = visible && slices.Contains(scope.Folders, f)
				}
				for _, policy := range mailbox.Policies {
					if policy.Operation == command.Operation && len(policy.Folders) > 0 {
						visible = visible && slices.Contains(policy.Folders, f)
					}
				}
				if visible {
					allowed = append(allowed, f)
				}
			}
			mailbox.AllowedFolders = allowed
			if len(allowed) == 0 {
				return api.Result{}, errs.Denied
			}
			return s.Provider.Read(ctx, mailbox, command)
		}
		return api.Result{Status: "ok", Mailboxes: []string{mailbox.Id}}, nil
	}
	if command.Operation == api.OperationHealth {
		if err := s.Receipts.Ready(ctx); err != nil {
			return api.Result{}, err
		}
		return s.Provider.Probe(ctx, mailbox), nil
	}
	if !Mutation(command.Operation) {
		return s.Provider.Read(ctx, mailbox, command)
	}
	if s.CompletionBase == nil || s.CompletionBase.Err() != nil || s.Effects == nil || s.Ledger == nil || s.Reports == nil {
		return api.Result{}, errs.Unavailable
	}
	id := fmt.Sprintf("%x", randomID())
	resource := ""
	if mailbox.ReceiveProtocol == "imap" && !Sending(command.Operation) && command.Uid != "" {
		resource = api.Digest(struct {
			Connection, Folder, UID string
			Validity                uint32
		}{mailbox.ConnectionId, folder, command.Uid, command.UidValidity})
	}
	audit := receipt.Audit{Actor: decision.ActorId, Agent: decision.AgentId, Grant: decision.GrantId, Operation: command.Operation, ConfigurationRevision: decision.ConfigurationRevision, CredentialGeneration: decision.CredentialGeneration, GateApproved: decision.GateApproved}
	source := receipt.ReportSource{Binding: request.ExecutionBinding, Connection: mailbox.ConnectionId}
	candidate := receipt.Record{ID: id, Key: command.EffectKey, Digest: request.InputSha256, Resource: resource, Audit: audit, Status: "unknown"}
	r, created, err := s.Reports.ReserveEffect(ctx, scope, candidate, source)
	if err != nil {
		return api.Result{}, err
	}
	if !created {
		// Исходный journal восстанавливает Report без подмены lineage новым invocation.
		return r.Result(), nil
	}
	if _, err := s.report(ctx, request.ExecutionBinding, scope, mailbox, r); err != nil {
		return api.Result{Status: "unknown", MessageId: r.ID}, errs.Unavailable
	}
	if ctx.Err() != nil || !source.Binding.Lease.ExpiresAt.After(time.Now()) {
		return api.Result{Status: "unknown", MessageId: r.ID}, errs.Denied
	}
	status := "unknown"
	if Sending(command.Operation) {
		status, err = s.Provider.Send(ctx, mailbox, command, r.ID)
	} else if mailbox.ReceiveProtocol == "imap" {
		var result api.Result
		result, err = s.Provider.Apply(ctx, mailbox, command, r.ID)
		status = result.Status
		r.UID, r.UIDValidity, r.Folder, r.ContentDigest = result.Uid, result.UidValidity, result.Folder, result.ContentDigest
	} else {
		status, err = s.Provider.Delete(ctx, mailbox, command.Uid)
	}
	// Отмена останавливает протокол, но не должна терять уже известные UID и outcome.
	if err != nil {
		status = "unknown"
	}
	completion, finish := context.WithTimeout(s.CompletionBase, receiptCompletionTimeout)
	defer finish()
	completed, err := s.Reports.CompleteEffect(completion, scope, r, status, source)
	if err != nil {
		return api.Result{Status: "unknown", MessageId: r.ID}, nil
	}
	r = completed
	if _, err := s.report(completion, request.ExecutionBinding, scope, mailbox, r); err != nil {
		return api.Result{Status: "unknown", MessageId: r.ID}, errs.Unavailable
	}
	return r.Result(), nil
}

func (s *Service) report(ctx context.Context, binding *api.ExecutionBinding, scope receipt.Scope, mailbox api.Mailbox, r receipt.Record) (receipt.OwnerReceipt, error) {
	pending := receipt.PendingReport{Scope: scope, Record: r, Source: receipt.ReportSource{Binding: binding, Connection: mailbox.ConnectionId}}
	if !pending.Valid() {
		return receipt.OwnerReceipt{}, errs.Invalid
	}
	confirmed, err := s.Effects.Report(ctx, pending.Report(false))
	if err != nil {
		return receipt.OwnerReceipt{}, err
	}
	after := binding.Lease.ExpiresAt.Add(receiptCompletionTimeout)
	if err := s.Ledger.Remember(ctx, scope, r, confirmed, after); err != nil {
		return receipt.OwnerReceipt{}, err
	}
	if err := s.Reports.AcknowledgeReport(ctx, pending); err != nil {
		return receipt.OwnerReceipt{}, err
	}
	return confirmed, nil
}

func randomID() []byte {
	v := make([]byte, 16)
	if _, e := rand.Read(v); e != nil {
		panic("random source unavailable")
	}
	return v
}
func authorize(m api.Mailbox, c api.Command, r api.AuthorizationRequest, d api.AuthorizationDecision) error {
	test := r.ExecutionBinding != nil && r.ExecutionBinding.ConnectionTestRef != nil
	if test && (c.Operation != api.OperationHealth || d.AgentId != "") {
		return errs.Denied
	}
	if r.ExecutionBinding != nil && api.Digest(r.ExecutionBinding) != api.Digest(d.ExecutionBinding) {
		return errs.Denied
	}
	if !d.Allowed || d.ActorId == "" || (!test && d.AgentId == "") || d.GrantId == "" || d.TenantId != m.TenantId || d.ConnectionId != m.ConnectionId || d.MailboxId != m.Id || d.Operation != c.Operation || d.InputSha256 != r.InputSha256 || d.EffectKey != c.EffectKey || d.ConfigurationRevision != m.Revision || d.CredentialGeneration != m.CredentialGeneration || d.ExpiresAt <= time.Now().Unix() || d.ExpiresAt > time.Now().Add(2*time.Minute).Unix() {
		return errs.Denied
	}
	policy := api.Deny
	for _, p := range m.Policies {
		if p.Operation == c.Operation {
			policy = p.Policy
			if c.Operation != api.OperationMailboxes && len(p.Folders) > 0 && (!slices.Contains(p.Folders, r.Folder) || (r.DestinationFolder != "" && !slices.Contains(p.Folders, r.DestinationFolder))) {
				return errs.Denied
			}
		}
	}
	if !d.Policy.Valid() || policy == api.Deny || d.Policy == api.Deny {
		return errs.Denied
	}
	scopes := []api.Scope{d.UserScope, d.ConnectionScope, d.ResourceScope}
	if !test {
		scopes = append(scopes, d.AgentScope)
	}
	for _, scope := range scopes {
		if scope.MailboxId != m.Id || scope.Sender != m.Sender || !slices.Contains(scope.Operations, c.Operation) || (c.Operation != api.OperationMailboxes && !slices.Contains(scope.Folders, r.Folder)) || (r.DestinationFolder != "" && !slices.Contains(scope.Folders, r.DestinationFolder)) {
			return errs.Denied
		}
		if Sending(c.Operation) || c.Operation == api.OperationDraftCreate || c.Operation == api.OperationDraftUpdate {
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
	out := []string{}
	if m.To != "" {
		out = append(out, m.To)
	}
	out = append(out, m.Cc...)
	return append(out, m.Bcc...)
}
func validateMessage(m api.Mailbox, c api.Command) error {
	v := c.Message
	if Sending(c.Operation) && !api.Address(v.To) {
		return errs.Invalid
	}
	if v.From != m.Sender || len(v.Subject) > 998 || strings.ContainsAny(v.Subject, "\r\n\x00") || len(v.BodyText) > m.Limits.MessageBytes || len(v.Attachments) > m.Limits.MaxAttachments || len(recipients(v)) > m.Limits.MaxRecipients {
		return errs.Invalid
	}
	for _, r := range recipients(v) {
		if !api.Address(r) || !slices.Contains(m.Recipients, r) {
			return errs.Denied
		}
	}
	if Sending(c.Operation) && c.Operation != api.OperationSend && (v.SourceUid == "" || len(v.SourceUid) > 70) {
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

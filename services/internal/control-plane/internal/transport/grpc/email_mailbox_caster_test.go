package grpc

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

func TestMailboxSpecificationTypedRoundtrip(t *testing.T) {
	endpoint := &cp.EmailMailboxEndpoint{Host: "mail.example.test", Port: 993, ServerName: "mail.example.test",
		TlsMode: cp.EmailMailboxTLSMode_EMAIL_MAILBOX_TLS_MODE_IMPLICIT, AuthMethod: cp.EmailMailboxAuthMethod_EMAIL_MAILBOX_AUTH_METHOD_OAUTHBEARER,
		Ca: &cp.EmailMailboxCredentialReference{Name: "email-ca", Generation: 3}, Username: &cp.EmailMailboxCredentialReference{Name: "email-user", Generation: 4}, Secret: &cp.EmailMailboxCredentialReference{Name: "email-auth", Generation: 5}}
	input := &cp.EmailMailboxSpecification{Enabled: true, ReceiveProtocol: cp.EmailMailboxReceiveProtocol_EMAIL_MAILBOX_RECEIVE_PROTOCOL_IMAP,
		AllowedFolders: []string{"INBOX", "Archive", "Drafts"}, ArchiveFolder: "Archive", DraftsFolder: "Drafts", Folder: "INBOX",
		Sender: "sender@example.test", ReplyTo: "reply@example.test", Recipients: []string{"receiver@example.test"}, HelloName: "mail.example.test",
		Smtp: proto.Clone(endpoint).(*cp.EmailMailboxEndpoint), Imap: endpoint, Pop: proto.Clone(endpoint).(*cp.EmailMailboxEndpoint),
		Limits: &cp.EmailMailboxLimits{AttachmentBytes: 100, MessageBytes: 200, MaxAttachments: 3, MaxRecipients: 4, PageSize: 5, ScanMessages: 6, TimeoutSeconds: 7}}
	for operation := int32(1); operation <= 21; operation++ {
		input.Policies = append(input.Policies, &cp.EmailMailboxOperationPolicy{Operation: cp.EmailOperation(operation), Policy: cp.EmailApprovalPolicy_EMAIL_APPROVAL_POLICY_HUMAN_GATE, Folders: []string{"INBOX"}})
	}
	internal, err := mailboxSpecification(input)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(input, castMailboxSpecification(internal)) {
		t.Fatal("typed mailbox fields lost during roundtrip")
	}
}

func TestMailboxSpecificationUnknownEnumsFailClosed(t *testing.T) {
	for _, mutate := range []func(*cp.EmailMailboxSpecification){
		func(s *cp.EmailMailboxSpecification) { s.ReceiveProtocol = 99 },
		func(s *cp.EmailMailboxSpecification) { s.Smtp = &cp.EmailMailboxEndpoint{TlsMode: 99} },
		func(s *cp.EmailMailboxSpecification) { s.Imap = &cp.EmailMailboxEndpoint{AuthMethod: 99} },
		func(s *cp.EmailMailboxSpecification) { s.Policies = []*cp.EmailMailboxOperationPolicy{{Operation: 99}} },
		func(s *cp.EmailMailboxSpecification) { s.Policies = []*cp.EmailMailboxOperationPolicy{{Policy: 99}} },
	} {
		spec := &cp.EmailMailboxSpecification{}
		mutate(spec)
		if _, err := mailboxSpecification(spec); err == nil {
			t.Fatal("unknown enum accepted")
		}
	}
	if _, err := mailboxSpecification(&cp.EmailMailboxSpecification{}); err != nil {
		t.Fatal("incomplete draft rejected")
	}
	if _, _, err := mailboxContent(nil); err == nil {
		t.Fatal("missing content oneof accepted")
	}
}

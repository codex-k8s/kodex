package httptransport

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

func mailboxSpecFixture() *cp.EmailMailboxSpecification {
	endpoint := &cp.EmailMailboxEndpoint{Host: "mail.fixture.invalid", Port: 465, ServerName: "mail.fixture.invalid", TlsMode: cp.EmailMailboxTLSMode_EMAIL_MAILBOX_TLS_MODE_IMPLICIT, AuthMethod: cp.EmailMailboxAuthMethod_EMAIL_MAILBOX_AUTH_METHOD_PASSWORD, Ca: &cp.EmailMailboxCredentialReference{Name: "cred_ca_fixture", Generation: 2}, Username: &cp.EmailMailboxCredentialReference{Name: "cred_user_fixture", Generation: 3}, Secret: &cp.EmailMailboxCredentialReference{Name: "cred_auth_fixture", Generation: 4}}
	return &cp.EmailMailboxSpecification{Enabled: true, ReceiveProtocol: cp.EmailMailboxReceiveProtocol_EMAIL_MAILBOX_RECEIVE_PROTOCOL_IMAP, AllowedFolders: []string{"INBOX", "Drafts"}, ArchiveFolder: "Archive", DraftsFolder: "Drafts", Folder: "INBOX", Sender: "sender@fixture.invalid", ReplyTo: "reply@fixture.invalid", Recipients: []string{"target@fixture.invalid"}, HelloName: "hello.fixture.invalid", Smtp: endpoint, Imap: proto.Clone(endpoint).(*cp.EmailMailboxEndpoint), Pop: proto.Clone(endpoint).(*cp.EmailMailboxEndpoint), Limits: &cp.EmailMailboxLimits{AttachmentBytes: 1024, MaxAttachments: 2, MaxRecipients: 3, MessageBytes: 4096, PageSize: 5, ScanMessages: 6, TimeoutSeconds: 7}, Policies: []*cp.EmailMailboxOperationPolicy{{Operation: cp.EmailOperation_EMAIL_OPERATION_SEND, Policy: cp.EmailApprovalPolicy_EMAIL_APPROVAL_POLICY_HUMAN_GATE, Folders: []string{"INBOX"}}}}
}

func TestMailboxSpecificationTypedRoundTripPreservesAllFieldsAndIncompleteDraft(t *testing.T) {
	for _, spec := range []*cp.EmailMailboxSpecification{mailboxSpecFixture(), {}, {Smtp: &cp.EmailMailboxEndpoint{}, Policies: []*cp.EmailMailboxOperationPolicy{{}}}} {
		view, ok := mailboxSpecificationView(spec)
		if !ok || view == nil {
			t.Fatal("typed draft view rejected")
		}
		result, ok := mailboxSpecificationInput(view)
		if !ok || !proto.Equal(result, spec) {
			t.Fatal("typed round trip lost mailbox field")
		}
		if spec.ReceiveProtocol == 0 && view.ReceiveProtocol != nil {
			t.Fatal("unspecified enum became selection")
		}
	}
	for value := range cp.EmailOperation_name {
		if value == 0 {
			continue
		}
		spec := mailboxSpecFixture()
		spec.Policies[0].Operation = cp.EmailOperation(value)
		view, ok := mailboxSpecificationView(spec)
		if !ok {
			t.Fatal("known operation lost")
		}
		result, ok := mailboxSpecificationInput(view)
		if !ok || !proto.Equal(result, spec) {
			t.Fatal("operation roundtrip changed")
		}
	}
}

func TestMailboxSpecificationRejectsUnknownEnumAndOversizedSnapshot(t *testing.T) {
	for _, mutate := range []func(*cp.EmailMailboxSpecification){
		func(v *cp.EmailMailboxSpecification) { v.ReceiveProtocol = 99 },
		func(v *cp.EmailMailboxSpecification) { v.Smtp.TlsMode = 99 },
		func(v *cp.EmailMailboxSpecification) { v.Imap.AuthMethod = 99 },
		func(v *cp.EmailMailboxSpecification) { v.Policies[0].Operation = 99 },
		func(v *cp.EmailMailboxSpecification) { v.Policies[0].Policy = 99 },
		func(v *cp.EmailMailboxSpecification) { v.Policies[0] = nil },
		func(v *cp.EmailMailboxSpecification) { v.Recipients = make([]string, 1001) },
		func(v *cp.EmailMailboxSpecification) { v.Policies[0].Folders = make([]string, 101) },
		func(v *cp.EmailMailboxSpecification) { v.Limits.MessageBytes = maximumSafeJSONInteger + 1 },
		func(v *cp.EmailMailboxSpecification) { v.Smtp.Secret.Generation = -1 },
	} {
		v := mailboxSpecFixture()
		mutate(v)
		if _, ok := mailboxSpecificationView(v); ok {
			t.Fatal("invalid producer shape accepted")
		}
	}
	view, _ := mailboxSpecificationView(mailboxSpecFixture())
	unknown := generated.EmailMailboxTLSMode("UNKNOWN")
	view.Smtp.TlsMode = &unknown
	if _, ok := mailboxSpecificationInput(view); ok {
		t.Fatal("unknown caller enum reached owner")
	}
}

func TestMailboxContentKeepsYAMLAsOwnerParsedInputAndRejectsTwoSources(t *testing.T) {
	yaml := "enabled: true\nreceive_protocol: imap\n"
	input, ok := mailboxContentInput(generated.EmailMailboxDraftContent{Yaml: &yaml})
	if !ok || input.GetYaml() != yaml {
		t.Fatal("YAML input was locally rewritten")
	}
	for _, content := range []generated.EmailMailboxDraftContent{{}, {Yaml: &yaml, Specification: &generated.EmailMailboxSpecification{}}} {
		if _, ok := mailboxContentInput(content); ok {
			t.Fatal("ambiguous content accepted")
		}
	}
}

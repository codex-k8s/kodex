package emailpolicy

import (
	"errors"
	"os"
	"strings"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func TestEmailAllExecutableOperationsUseExactSemanticScopeAndMailboxGate(t *testing.T) {
	raw, err := os.ReadFile("../../../../../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	projection, err := DecodeConfiguration(raw)
	if err != nil {
		t.Fatal(err)
	}
	mailbox := projection.Mailboxes[0]
	for index := range mailbox.Policies {
		mailbox.Policies[index].Policy = api.HumanGate
	}
	packages, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := packages["email"]
	if len(definition.Spec.Capabilities) != 21 {
		t.Fatal("email executable catalog changed without owner matrix")
	}
	mutations := 0
	for _, capability := range definition.Spec.Capabilities {
		t.Run(capability.Operation, func(t *testing.T) {
			bounded := []byte(`{}`)
			switch capability.Operation {
			case "email.message.status.read":
				bounded = []byte(`{"message_id":"fixture-message"}`)
			case "email.message.move":
				bounded = []byte(`{"destination_folder":"Archive"}`)
			case "email.message.send", "email.message.reply", "email.message.reply_all", "email.message.forward", "email.draft.create", "email.draft.update":
				bounded = []byte(`{"to":"recipient@example.test","cc":"[\"copy@example.test\"]","subject":"Fixture","body_text":"Body"}`)
			}
			command, err := api.CommandForIntegration(capability.Operation, mailbox.Ref, mailbox.Sender, "opaque:effect", bounded)
			if err != nil {
				t.Fatal(err)
			}
			if api.IsMutation(command.Operation) {
				mutations++
			}
			input := query.EmailAuthorization{MailboxRef: mailbox.Ref, Sender: mailbox.Sender, ConfigurationRevision: mailbox.Revision,
				Operation: string(command.Operation), EffectKey: command.EffectKey, SemanticInputDigest: api.Digest(command), Folder: mailbox.Folder,
				DestinationFolder: command.DestinationFolder}
			switch command.Operation {
			case api.OperationDraftCreate, api.OperationDraftUpdate, api.OperationDraftDelete:
				input.Folder = mailbox.DraftsFolder
			case api.OperationArchive:
				input.DestinationFolder = mailbox.ArchiveFolder
			}
			gate, err := CommandRequiresGate(mailbox, capability.Operation, "opaque:effect", bounded)
			if err != nil || !gate {
				t.Fatalf("mailbox gate omitted: %v", err)
			}
			if _, _, err := AuthorizeCommand(mailbox, capability.Operation, "opaque:effect", bounded, input, false); !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("unapproved mailbox gate: %v", err)
			}
			scope, policy, err := AuthorizeCommand(mailbox, capability.Operation, "opaque:effect", bounded, input, true)
			if err != nil || policy != string(api.HumanGate) || len(scope.Operations) != 1 || scope.Operations[0] != input.Operation {
				t.Fatalf("exact operation: %v", err)
			}
			for _, configured := range []api.Policy{api.Allow, api.Deny} {
				configuredMailbox := mailbox
				configuredMailbox.Policies = append(configuredMailbox.Policies[:0:0], mailbox.Policies...)
				for index := range configuredMailbox.Policies {
					configuredMailbox.Policies[index].Policy = configured
				}
				gate, gateErr := CommandRequiresGate(configuredMailbox, capability.Operation, "opaque:effect", bounded)
				_, effective, authorizationErr := AuthorizeCommand(configuredMailbox, capability.Operation, "opaque:effect", bounded, input, false)
				if configured == api.Allow && (gateErr != nil || gate || authorizationErr != nil || effective != string(api.Allow)) {
					t.Fatalf("explicit mailbox ALLOW required approval: gate=%v err=%v authorization=%v", gate, gateErr, authorizationErr)
				}
				if configured == api.Deny && (!errors.Is(gateErr, errs.ErrForbidden) || !errors.Is(authorizationErr, errs.ErrForbidden)) {
					t.Fatalf("mailbox DENY accepted: gate=%v authorization=%v", gateErr, authorizationErr)
				}
			}
			for _, field := range []string{"digest", "sender", "folder", "effect", "destination"} {
				bad := input
				switch field {
				case "digest":
					bad.SemanticInputDigest = strings.Repeat("e", 64)
				case "sender":
					bad.Sender = "foreign@example.test"
				case "folder":
					bad.Folder = "Foreign"
				case "effect":
					bad.EffectKey += "other"
				case "destination":
					bad.DestinationFolder = "Foreign"
				}
				if _, _, err := AuthorizeCommand(mailbox, capability.Operation, "opaque:effect", bounded, bad, true); !errors.Is(err, errs.ErrForbidden) {
					t.Fatalf("changed %s accepted: %v", field, err)
				}
			}
		})
	}
	if mutations != 12 {
		t.Fatalf("mutation matrix=%d", mutations)
	}
}

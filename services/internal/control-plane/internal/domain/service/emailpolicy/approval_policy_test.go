package emailpolicy

import (
	"errors"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestEmailApprovalFloorCannotBeWeakenedByMailboxAllow checks every shipped operation against the selected NONE/SCOPED/EACH matrix, including stricter mailbox gates and deny rules.
// TestEmailApprovalFloorCannotBeWeakenedByMailboxAllow проверяет каждую поставляемую операцию по выбранной матрице NONE/SCOPED/EACH, включая более строгие gates и запреты mailbox.
func TestEmailApprovalFloorCannotBeWeakenedByMailboxAllow(t *testing.T) {
	raw, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	spec := mailboxSpecificationFixture(t)
	mailbox := MailboxProjection{Ref: "mailbox-fixture", Sender: spec.Sender, Policies: spec.Policies, Enabled: true, Revision: 1,
		AllowedFolders: spec.AllowedFolders, Recipients: spec.Recipients, Folder: spec.Folder, DraftsFolder: spec.DraftsFolder, ArchiveFolder: spec.ArchiveFolder}
	counts := map[integrationpackage.ApprovalPolicy]int{}
	for _, capability := range raw["email"].Spec.Capabilities {
		t.Run(capability.Operation, func(t *testing.T) {
			minimum, err := MinimumApprovalPolicy(capability.Operation)
			if err != nil {
				t.Fatal(err)
			}
			counts[minimum]++
			input := []byte(`{}`)
			if capability.Operation == "email.message.status.read" {
				input = []byte(`{"message_id":"fixture-message"}`)
			}
			if capability.Operation == "email.message.move" {
				input = []byte(`{"destination_folder":"Archive"}`)
			}
			for _, configured := range []api.Policy{api.Allow, api.Deny, api.HumanGate} {
				current := mailbox
				current.Policies = append([]api.OperationPolicy{}, mailbox.Policies...)
				for index := range current.Policies {
					current.Policies[index].Policy = configured
				}
				for _, selected := range []integrationpackage.ApprovalPolicy{integrationpackage.ApprovalNone, integrationpackage.ApprovalHumanScoped, integrationpackage.ApprovalHumanEachEffect} {
					want := configured != api.Deny && (selected == minimum || selected == integrationpackage.ApprovalHumanEachEffect)
					if configured == api.HumanGate && minimum != integrationpackage.ApprovalNone {
						want = selected == integrationpackage.ApprovalHumanEachEffect
					}
					actual := ValidateCommandApprovalPolicy(current, capability.Operation, input, string(selected))
					if (actual == nil) != want {
						t.Fatalf("mailbox=%s selected=%s floor=%s allowed=%t err=%v", configured, selected, minimum, want, actual)
					}
				}
			}
		})
	}
	if counts[integrationpackage.ApprovalNone] != 9 || counts[integrationpackage.ApprovalHumanScoped] != 7 || counts[integrationpackage.ApprovalHumanEachEffect] != 5 {
		t.Fatal("email operation matrix changed without explicit review", counts)
	}
	for _, operation := range []string{"", "email.message.mark", "email.message.send.extra", "EMAIL.MESSAGE.SEND"} {
		if _, err := MinimumApprovalPolicy(operation); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("unknown operation acquired approval policy")
		}
	}
}

// TestEmailScopeFoldersUseOwnerDefaults keeps draft and archive scopes on authoritative folders rather than trusting omitted or caller-chosen aliases.
// TestEmailScopeFoldersUseOwnerDefaults сохраняет scope черновиков и архива на авторитетных папках, не полагаясь на отсутствующие поля либо псевдонимы caller.
func TestEmailScopeFoldersUseOwnerDefaults(t *testing.T) {
	mailbox := MailboxProjection{Ref: "mailbox-fixture", Sender: "user@example.test", Folder: "INBOX", DraftsFolder: "Drafts", ArchiveFolder: "Archive"}
	for _, test := range []struct{ operation, input, folder, destination string }{
		{"email.draft.create", `{}`, "Drafts", ""},
		{"email.draft.update", `{}`, "Drafts", ""},
		{"email.draft.delete", `{}`, "Drafts", ""},
		{"email.message.archive", `{}`, "INBOX", "Archive"},
		{"email.message.move", `{"folder":"Other","destination_folder":"Archive"}`, "Other", "Archive"},
		{"email.message.mark_read", `{}`, "INBOX", ""},
	} {
		folder, destination, err := ScopedCommandFolders(mailbox, test.operation, []byte(test.input))
		if err != nil || folder != test.folder || destination != test.destination {
			t.Fatalf("operation=%s folder=%q destination=%q err=%v", test.operation, folder, destination, err)
		}
	}
}

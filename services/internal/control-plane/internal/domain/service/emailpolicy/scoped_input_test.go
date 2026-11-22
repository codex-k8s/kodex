package emailpolicy

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// emailScopedInputFixture creates a complete current scoped input with explicit recipient absence, folder and provider UID validity, without any transport or credential values.
// emailScopedInputFixture создаёт полный текущий scoped input с явным отсутствием получателей, папкой и provider UID validity без transport либо credential values.
func emailScopedInputFixture(operation string) map[string]any {
	input := map[string]any{"folder": "INBOX", "uid": "42", "uid_validity": int64(1)}
	switch operation {
	case "email.message.move", "email.message.archive":
		input["destination_folder"] = "Archive"
	case "email.draft.create", "email.draft.update":
		input["folder"], input["to"], input["cc"], input["bcc"] = "Drafts", "", "[]", "[]"
		if operation == "email.draft.create" {
			delete(input, "uid")
			delete(input, "uid_validity")
		} else {
			input["expected_digest"] = strings.Repeat("a", 64)
		}
	case "email.draft.delete":
		input["folder"] = "Drafts"
	}
	return input
}

// TestEmailScopedInputsBindEveryResourceCoordinate exercises the exact shipped contract, rejecting missing coordinates and proving that changed folders, cc/bcc, recipients or destinations cannot reuse the first consent digest.
// TestEmailScopedInputsBindEveryResourceCoordinate проверяет точный shipped contract, отклоняет отсутствующие coordinates и доказывает, что новые папки, cc/bcc, получатели либо назначения не переиспользуют первый consent digest.
func TestEmailScopedInputsBindEveryResourceCoordinate(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, capability := range definitions["email"].Spec.Capabilities {
		minimum, err := MinimumApprovalPolicy(capability.Operation)
		if err != nil {
			t.Fatal(err)
		}
		if minimum != integrationpackage.ApprovalHumanScoped {
			if minimum == integrationpackage.ApprovalNone && capability.AllowsApprovalPolicy(string(integrationpackage.ApprovalHumanScoped)) {
				t.Fatal("read inherited write consent")
			}
			continue
		}
		count++
		t.Run(capability.Operation, func(t *testing.T) {
			input := emailScopedInputFixture(capability.Operation)
			raw, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			paths := capability.ApprovalScope.RequiredInputPaths
			first, err := capability.ResolveApprovalScope(paths, raw)
			if err != nil {
				t.Fatal("complete scoped input rejected", err)
			}
			for _, path := range paths {
				key := strings.TrimPrefix(path, "/")
				original := input[key]
				delete(input, key)
				missing, _ := json.Marshal(input)
				if _, err := capability.ResolveApprovalScope(paths, missing); err == nil {
					t.Fatal("missing scope coordinate accepted", key)
				}
				input[key] = original
				switch key {
				case "folder", "destination_folder":
					input[key] = "Other"
				case "to":
					input[key] = "recipient@example.test"
				case "cc", "bcc":
					input[key] = `["copy@example.test"]`
				default:
					t.Fatal("unreviewed required scope coordinate", key)
				}
				changed, _ := json.Marshal(input)
				second, err := capability.ResolveApprovalScope(paths, changed)
				if err != nil || first.Digest == second.Digest {
					t.Fatal("hidden resource change retained consent", key, err)
				}
				input[key] = original
			}
			if capability.Operation == "email.draft.create" || capability.Operation == "email.draft.update" {
				input["subject"], input["body_text"] = "Новая тема", "Новый текст"
				edited, _ := json.Marshal(input)
				second, err := capability.ResolveApprovalScope(paths, edited)
				if err != nil || first.Digest != second.Digest {
					t.Fatal("allowed draft edit lost resource consent", err)
				}
			}
		})
	}
	if count != 7 {
		t.Fatal("scoped operation matrix changed", count)
	}
}

// TestEmailResourceBoundsRejectArchiveRetargetAndHiddenRecipients proves backend validation precedes a proposed gate, including malformed cc JSON and a destination that the bridge would otherwise replace with its configured archive.
// TestEmailResourceBoundsRejectArchiveRetargetAndHiddenRecipients доказывает backend validation до предлагаемого gate, включая неверный cc JSON и назначение, которое bridge иначе заменил бы настроенным архивом.
func TestEmailResourceBoundsRejectArchiveRetargetAndHiddenRecipients(t *testing.T) {
	spec := mailboxSpecificationFixture(t)
	mailbox := MailboxProjection{Ref: "mailbox-fixture", Sender: spec.Sender, Revision: 1, Enabled: true, Folder: spec.Folder, DraftsFolder: spec.DraftsFolder, ArchiveFolder: spec.ArchiveFolder,
		AllowedFolders: spec.AllowedFolders, Recipients: spec.Recipients, Policies: append([]api.OperationPolicy{}, spec.Policies...)}
	for index := range mailbox.Policies {
		mailbox.Policies[index].Policy = api.Allow
	}
	for _, test := range []struct {
		operation, input string
		allowed          bool
	}{
		{"email.message.archive", `{"folder":"INBOX","destination_folder":"Archive"}`, true},
		{"email.message.archive", `{"folder":"INBOX","destination_folder":"Drafts"}`, false},
		{"email.draft.create", `{"folder":"Drafts","to":"","cc":"[]","bcc":"[]"}`, true},
		{"email.draft.create", `{"folder":"Drafts","to":"unapproved@example.test","cc":"[]","bcc":"[]"}`, false},
		{"email.draft.update", `{"folder":"Drafts","to":"","cc":"[\"unapproved@example.test\"]","bcc":"[]"}`, false},
		{"email.draft.create", `{"folder":"Drafts","to":"","cc":"[]","bcc":"[\"unapproved@example.test\"]"}`, false},
		{"email.draft.create", `{"folder":"Drafts","to":"","cc":"null","bcc":"[]"}`, false},
	} {
		if err := ValidateCommandResourceBounds(mailbox, test.operation, []byte(test.input)); (err == nil) != test.allowed {
			t.Fatalf("operation=%s allowed=%t err=%v", test.operation, test.allowed, err)
		}
	}
}

package grpc

import (
	"os/exec"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Fixture вызывает реальный JS helper; HTTP/provider не подменяют MaterializeMailbox.
func TestOwnerEmailHelperMaterialization(t *testing.T) {
	for _, protocol := range []string{"IMAP", "POP3"} {
		t.Run(protocol, func(t *testing.T) {
			raw, err := exec.Command("node", "../../../../../../tools/dev/email-mailbox-materialization-fixture.mjs", protocol).Output()
			if err != nil {
				t.Fatal("helper fixture failed", err)
			}
			var input cp.EmailMailboxSpecification
			if err := protojson.Unmarshal(raw, &input); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name   string
				mutate func(*cp.EmailMailboxSpecification)
				valid  bool
			}{
				{"complete", func(*cp.EmailMailboxSpecification) {}, true},
				{"missing_reply_to", func(s *cp.EmailMailboxSpecification) { s.ReplyTo = "" }, false},
				{"missing_policy", func(s *cp.EmailMailboxSpecification) { s.Policies = s.Policies[1:] }, false},
				{"duplicate_policy", func(s *cp.EmailMailboxSpecification) {
					s.Policies[1] = proto.Clone(s.Policies[0]).(*cp.EmailMailboxOperationPolicy)
				}, false},
				{"unknown_policy", func(s *cp.EmailMailboxSpecification) { s.Policies[0].Operation = 99 }, false},
				{"missing_ca", func(s *cp.EmailMailboxSpecification) { s.Smtp.Ca = nil }, false},
				{"wrong_sni", func(s *cp.EmailMailboxSpecification) { s.Smtp.ServerName = "other.example.invalid" }, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					candidate := proto.Clone(&input).(*cp.EmailMailboxSpecification)
					tc.mutate(candidate)
					original := proto.Clone(candidate)
					spec, err := mailboxSpecification(candidate)
					if err != nil {
						if tc.valid {
							t.Fatal(err)
						}
						return
					}
					mailbox, err := emailpolicy.MaterializeMailbox(spec, emailpolicy.MailboxBinding{Ref: "mailbox-fixture", OrganizationRef: "org-fixture", ConnectionRef: "connection-fixture", Revision: 2, CredentialGeneration: 7})
					if (err == nil) != tc.valid {
						t.Fatalf("materialization acceptance mismatch: valid=%t err=%v", tc.valid, err)
					}
					if !tc.valid {
						return
					}
					if len(spec.Policies) != 21 || len(mailbox.Policies) != 22 || !proto.Equal(original, castMailboxSpecification(spec)) {
						t.Fatal("public policy or immutable input changed")
					}
					legacy := 0
					for _, policy := range mailbox.Policies {
						if policy.Operation == api.OperationMark {
							legacy++
							if policy.Policy != api.Deny {
								t.Fatal("legacy mark gained authority")
							}
						}
					}
					if legacy != 1 {
						t.Fatal("legacy deny policy missing")
					}
				})
			}
		})
	}
}

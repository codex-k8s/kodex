package component

import (
	"errors"
	"strings"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
)

func TestIMAPAuthorityBeforeCredentials(t *testing.T) {
	f := newFixture(t, "implicit")
	s, sec, authority := service(t, f, "implicit", nil)
	m := &s.Config.Mailboxes[0]
	endpoint := m.Smtp
	endpoint.Port = 993
	m.Imap = &endpoint
	m.ReceiveProtocol = "imap"
	m.AllowedFolders = []string{"INBOX", "Archive", "Drafts"}
	m.DraftsFolder = "Drafts"
	m.ArchiveFolder = "Archive"
	for _, op := range api.Operations() {
		if op == api.OperationMark {
			continue
		}
		t.Run(string(op), func(t *testing.T) {
			cmd := send(op, "effect")
			cmd.Uid = "1"
			cmd.UidValidity = 1
			cmd.Message.SourceUid = "1"
			cmd.Message.SourceUidValidity = 1
			cmd.ThreadId = "source@example.test"
			cmd.ExpectedDigest = strings.Repeat("a", 64)
			if op == api.OperationMove {
				cmd.DestinationFolder = "Archive"
			}
			for _, boundary := range []string{"user", "agent", "connection", "resource", "destination", "gate", "revocation"} {
				t.Run(boundary, func(t *testing.T) {
					if boundary == "destination" && op != api.OperationMove && op != api.OperationArchive {
						return
					}
					authority.revoked = boundary == "revocation"
					authority.mutate = func(d *api.AuthorizationDecision) {
						for _, scope := range []*api.Scope{&d.UserScope, &d.AgentScope, &d.ConnectionScope, &d.ResourceScope} {
							scope.Folders = append([]string(nil), m.AllowedFolders...)
						}
						switch boundary {
						case "user":
							d.UserScope.Folders = nil
						case "agent":
							d.AgentScope.Folders = nil
						case "connection":
							d.ConnectionScope.Folders = nil
						case "resource":
							d.ResourceScope.Folders = nil
						case "destination":
							d.ResourceScope.Folders = []string{"INBOX", "Drafts"}
						case "gate":
							d.Policy = api.HumanGate
							d.GateApproved = false
						}
					}
					before := sec.reads.Load()
					_, err := s.Execute(executionContext(t.Context()), "caller", "token", cmd)
					want := errs.Denied
					if boundary == "gate" {
						want = errs.Gate
					}
					if !errors.Is(err, want) || sec.reads.Load() != before {
						t.Fatalf("boundary %s: %v", boundary, err)
					}
				})
			}
		})
	}
}

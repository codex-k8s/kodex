package component

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
	httptransport "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http"
)

// Тот же mapping используется gateway и producer для immutable input digest.
func httpsInvoker(t *testing.T, f *providerFixture, s *mail.Service) func(string, string, map[string]any) api.Result {
	t.Helper()
	server := httptest.NewUnstartedServer(httptransport.Handler{Service: s})
	ca := x509.NewCertPool()
	ca.AppendCertsFromPEM(f.ca)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{f.cert}, ClientCAs: ca, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: ca, Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS12}}}
	t.Cleanup(httpClient.CloseIdleConnections)
	client, err := api.NewClient(server.URL, api.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(operation, effect string, input map[string]any) api.Result {
		t.Helper()
		raw, _ := json.Marshal(input)
		command, err := api.CommandForIntegration(operation, "mailbox", "sender@example.test", effect, raw)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.ExecuteMailboxOperation(t.Context(), command, func(_ context.Context, request *http.Request) error {
			request.Header.Set("Authorization", "Bearer fixture")
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d", operation, response.StatusCode)
		}
		var result api.Result
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	return invoke
}

func TestTypedCatalogHTTPS(t *testing.T) {
	f := newFixture(t, "starttls")
	s, _, _ := service(t, f, "starttls", nil)
	invoke := httpsInvoker(t, f, s)
	for _, operation := range []string{"email.delivery.health.read", "email.mailbox.list", "email.message.list", "email.message.search", "email.message.read", "email.attachment.read"} {
		input := map[string]any{}
		if operation == "email.message.search" {
			input["query"] = "search"
		}
		if operation == "email.message.read" || operation == "email.attachment.read" {
			input["uid"] = "uid-one"
		}
		result := invoke(operation, "", input)
		if result.Status != "ok" && result.Status != "ready" {
			t.Fatalf("%s: %s", operation, result.Status)
		}
		if operation == "email.attachment.read" && (len(result.Attachments) != 1 || result.Attachments[0].ContentBase64 != "YXR0YWNobWVudA==") {
			t.Fatal("download bytes differ")
		}
	}
	for _, operation := range []string{"email.message.send", "email.message.reply", "email.message.reply_all", "email.message.forward"} {
		input := map[string]any{"to": "recipient@example.test", "subject": "Fixture", "body_text": "Body", "attachments": `[{"filename":"note.txt","content_type":"text/plain","content_base64":"aGVsbG8="}]`}
		if operation != "email.message.send" {
			input["source_uid"] = "uid-one"
		}
		if operation == "email.message.reply_all" {
			input["cc"] = `["copy@example.test"]`
		}
		first := invoke(operation, operation, input)
		if first.Status != "accepted" {
			t.Fatalf("%s: %s", operation, first.Status)
		}
		if repeat := invoke(operation, operation, input); repeat.MessageId != first.MessageId || repeat.Status != first.Status {
			t.Fatal("duplicate receipt differs")
		}
		if receipt := invoke("email.message.status.read", "", map[string]any{"message_id": first.MessageId}); receipt.Status != "accepted" {
			t.Fatal("status mapping")
		}
		if receipt := invoke("email.message.status.read", "", map[string]any{"effect_key": operation}); receipt.MessageId != first.MessageId || receipt.Status != "accepted" {
			t.Fatal("effect reconciliation mapping")
		}
	}
	if result := invoke("email.message.delete", "delete", map[string]any{"uid": "uid-one"}); result.Status != "deleted" {
		t.Fatal("delete mapping")
	}
	f.mu.Lock()
	f.dropSMTP = true
	f.mu.Unlock()
	unknownInput := map[string]any{"to": "recipient@example.test", "subject": "Fixture", "body_text": "Body"}
	unknown := invoke("email.message.send", "unknown", unknownInput)
	if unknown.Status != "unknown" {
		t.Fatal("ambiguous SMTP outcome")
	}
	if receipt := invoke("email.message.status.read", "", map[string]any{"effect_key": "unknown"}); receipt.Status != "unknown" || receipt.MessageId != unknown.MessageId {
		t.Fatal("unknown receipt lookup")
	}
	if repeat := invoke("email.message.send", "unknown", unknownInput); repeat.Status != "unknown" || repeat.MessageId != unknown.MessageId {
		t.Fatal("unknown receipt replay")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) != 5 || f.deletes != 1 {
		t.Fatalf("effects SMTP=%d POP=%d", len(f.sent), f.deletes)
	}
}

func TestTypedIMAPCatalogHTTPS(t *testing.T) {
	f := newFixture(t, "starttls")
	_, address := imapFixture(t, f, "starttls")
	s, sec, auth := service(t, f, "starttls", nil)
	m := &s.Config.Mailboxes[0]
	endpoint := m.Smtp
	endpoint.Port = 143
	m.Imap = &endpoint
	m.ReceiveProtocol = "imap"
	m.AllowedFolders = []string{"INBOX", "Archive", "Drafts"}
	m.ArchiveFolder = "Archive"
	m.DraftsFolder = "Drafts"
	auth.mutate = func(d *api.AuthorizationDecision) {
		for _, scope := range []*api.Scope{&d.UserScope, &d.AgentScope, &d.ConnectionScope, &d.ResourceScope} {
			scope.Folders = m.AllowedFolders
		}
	}
	s.Provider = &mailtransport.Provider{Secrets: sec, Dialer: imapDialFixture{dialFixture{f.smtp, f.pop}, address}}
	call := httpsInvoker(t, f, s)
	seen := map[string]bool{}
	invoke := func(op, key string, input map[string]any) api.Result {
		t.Helper()
		seen[op] = true
		r := call(op, key, input)
		if r.Status != "ok" && r.Status != "ready" && r.Status != "accepted" && r.Status != "deleted" {
			t.Fatalf("%s: %s", op, r.Status)
		}
		if key != "" {
			retry := call(op, key, input)
			if api.Digest(retry) != api.Digest(r) {
				t.Fatal("HTTPS receipt replay changed")
			}
		}
		return r
	}
	invoke("email.delivery.health.read", "", map[string]any{})
	invoke("email.mailbox.list", "", map[string]any{})
	list := invoke("email.message.list", "", map[string]any{})
	validity := list.UidValidity
	invoke("email.message.search", "", map[string]any{"query": "Original"})
	invoke("email.thread.read", "", map[string]any{"thread_id": "source@example.test"})
	for _, op := range []string{"email.message.read", "email.attachment.list", "email.attachment.read", "email.message.mark_read", "email.message.mark_unread"} {
		input := map[string]any{"uid": "3", "uid_validity": validity}
		if op == "email.attachment.read" {
			input["attachment_index"] = 0
		}
		key := ""
		if op == "email.message.mark_read" || op == "email.message.mark_unread" {
			key = op
		}
		invoke(op, key, input)
	}
	for _, op := range []string{"email.message.send", "email.message.reply", "email.message.reply_all", "email.message.forward"} {
		input := map[string]any{"to": "recipient@example.test", "subject": "Fixture", "body_text": "Body"}
		if op != "email.message.send" {
			input["source_uid"] = "3"
			input["source_uid_validity"] = validity
		}
		if op == "email.message.reply_all" {
			input["cc"] = `["copy@example.test"]`
		}
		r := invoke(op, op, input)
		invoke("email.message.status.read", "", map[string]any{"message_id": r.MessageId})
	}
	draft := invoke("email.draft.create", "create", map[string]any{"body_text": "Draft without recipients"})
	draft = invoke("email.draft.update", "update", map[string]any{"uid": draft.Uid, "uid_validity": draft.UidValidity, "expected_digest": draft.ContentDigest, "body_text": "Updated draft"})
	invoke("email.draft.delete", "delete-draft", map[string]any{"uid": draft.Uid, "uid_validity": draft.UidValidity})
	invoke("email.message.move", "move", map[string]any{"uid": "1", "uid_validity": validity, "destination_folder": "Archive"})
	invoke("email.message.archive", "archive", map[string]any{"uid": "2", "uid_validity": validity})
	invoke("email.message.delete", "delete", map[string]any{"uid": "3", "uid_validity": validity})
	if len(seen) != 21 {
		t.Fatalf("advertised HTTPS operations covered: %d", len(seen))
	}
}

package component

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	projection "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/configuration"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
)

func projectMailbox(t *testing.T, root string, value api.Configuration, ca []byte) *projection.Snapshot {
	t.Helper()
	name := "..revision-" + strconv.FormatInt(value.Revision, 10)
	directory := filepath.Join(root, name)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for key, data := range map[string][]byte{"mailboxes.json": raw, "ca.1": ca, "user.1": []byte("fixture-value"), "password.1": []byte("fixture-value")} {
		if err := os.WriteFile(filepath.Join(directory, key), data, 0440); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(name, filepath.Join(root, "..next")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "..next"), filepath.Join(root, "..data")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := projection.Load(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestMailboxProjectionReloadHTTPS(t *testing.T) {
	store := postgresFixture(t)
	f := newFixture(t, "starttls")
	base, _, authority := service(t, f, "starttls", store)
	root := t.TempDir()
	var current atomic.Pointer[mail.Service]
	var observedRevision atomic.Int64
	authority.mutate = func(decision *api.AuthorizationDecision) { observedRevision.Store(decision.ConfigurationRevision) }
	activate := func(snapshot *projection.Snapshot) *mail.Service {
		t.Helper()
		if err := store.Configuration(t.Context(), snapshot.Configuration, api.Digest(snapshot.Configuration)); err != nil {
			t.Fatal("projection watermark rejected")
		}
		next := *base
		next.Config = snapshot.Configuration
		next.Provider = &mailtransport.Provider{Secrets: snapshot, Dialer: dialFixture{f.smtp, f.pop}}
		current.Store(&next)
		return &next
	}
	firstSnapshot := projectMailbox(t, root, base.Config, f.ca)
	firstService := activate(firstSnapshot)
	invoke := httpsInvoker(t, f, base, current.Load)
	if result := invoke("email.message.read", "", map[string]any{"uid": "uid-one"}); result.Uid != "uid-one" || observedRevision.Load() != 1 {
		t.Fatal("initial snapshot not served")
	}
	nextConfig := configuration("starttls")
	nextConfig.Revision, nextConfig.Mailboxes[0].Revision = 2, 2
	activate(projectMailbox(t, root, nextConfig, f.ca))
	if result := invoke("email.message.read", "", map[string]any{"uid": "uid-two"}); result.Uid != "uid-two" || observedRevision.Load() != 2 {
		t.Fatal("new snapshot not served without restart")
	}
	if firstService.Config.Revision != 1 {
		t.Fatal("reload mutated existing request snapshot")
	}
	if err := store.Configuration(t.Context(), firstSnapshot.Configuration, api.Digest(firstSnapshot.Configuration)); err == nil {
		t.Fatal("old request bypassed durable watermark")
	}
	copy := *store
	if err := copy.Configuration(t.Context(), firstSnapshot.Configuration, api.Digest(firstSnapshot.Configuration)); err == nil {
		t.Fatal("replacement adapter accepted rollback")
	}
	changed := nextConfig
	changed.Source = "foreign-same-revision"
	if err := copy.Configuration(t.Context(), changed, api.Digest(changed)); err == nil {
		t.Fatal("same revision digest replacement accepted")
	}
}

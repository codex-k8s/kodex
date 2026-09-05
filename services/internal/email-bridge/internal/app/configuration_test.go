package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/configuration"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
)

func TestConfigurationDeploymentPins(t *testing.T) {
	seed := api.Configuration{Version: "email-bridge/v1", Revision: 1, ManagedBy: "git", Source: "release-bootstrap", Mailboxes: []api.Mailbox{}}
	for _, name := range []string{"bootstrap", "managed", "unknown", "unpinned", "negative-revision", "uppercase-digest", "bootstrap-pin", "bootstrap-source", "bootstrap-revision", "bootstrap-owner", "bootstrap-mailbox", "managed-revision", "managed-digest"} {
		t.Run(name, func(t *testing.T) {
			value := seed
			pins := configurationPins{mode: configurationBootstrap}
			switch name {
			case "managed":
				pins = configurationPins{mode: configurationManaged, revision: 1, digest: api.Digest(value)}
			case "unknown":
				pins.mode = ""
			case "unpinned":
				pins.mode = configurationManaged
			case "negative-revision":
				pins = configurationPins{mode: configurationManaged, revision: -1, digest: api.Digest(value)}
			case "uppercase-digest":
				pins = configurationPins{mode: configurationManaged, revision: 1, digest: strings.Repeat("A", 64)}
			case "bootstrap-pin":
				pins.revision = 1
			case "bootstrap-source":
				value.Source = "ui"
			case "bootstrap-revision":
				value.Revision = 2
			case "bootstrap-owner":
				value.ManagedBy = "ui"
			case "bootstrap-mailbox":
				value.Mailboxes = []api.Mailbox{{}}
			case "managed-revision":
				pins = configurationPins{mode: configurationManaged, revision: 2, digest: api.Digest(value)}
			case "managed-digest":
				pins = configurationPins{mode: configurationManaged, revision: 1, digest: strings.Repeat("a", 64)}
			}
			if (pins.check(value, api.Digest(value)) == nil) != (name == "bootstrap" || name == "managed") {
				t.Fatal("deployment pin validation mismatch")
			}
		})
	}
}

func TestManagedReadbackBeforeServingAndRecovery(t *testing.T) {
	root := t.TempDir()
	mountConfiguration(t, root, "..first", 7)
	value := api.Configuration{Version: "email-bridge/v1", Revision: 7, ManagedBy: "git", Source: "fixture", Mailboxes: []api.Mailbox{}}
	pins := configurationPins{mode: configurationManaged, revision: value.Revision, digest: api.Digest(value)}
	accepted, built, reports := false, false, 0
	failure := true
	runtime := &configurationRuntime{root: root, check: pins.check, accept: func(context.Context, api.Configuration, string) error {
		accepted = true
		return nil
	}, build: func(snapshot *configuration.Snapshot) *mail.Service {
		built = true
		return &mail.Service{Config: snapshot.Configuration}
	}}
	runtime.report = func(_ context.Context, revision int64, digest string) error {
		reports++
		if !accepted || !built || revision != pins.revision || digest != pins.digest {
			t.Fatal("unaccepted or unpinned snapshot reported")
		}
		if failure {
			return errors.New("owner unavailable")
		}
		return nil
	}
	if runtime.Refresh(t.Context()) == nil || runtime.Service() != nil || reports != 1 {
		t.Fatal("missing owner acknowledgement allowed requests")
	}
	failure = false
	if err := runtime.Refresh(t.Context()); err != nil || runtime.Service() == nil || reports != 2 {
		t.Fatal("idempotent readback did not recover")
	}
	old := runtime.Service()
	mountConfiguration(t, root, "..unapproved", 8)
	accepted, built = false, false
	if runtime.Refresh(t.Context()) == nil || runtime.Service() != nil || accepted || built || reports != 2 || old.Config.Revision != 7 {
		t.Fatal("unapproved projection passed watermark or replaced in-flight snapshot")
	}
}

func mountConfiguration(t *testing.T, root, name string, revision int64) {
	t.Helper()
	directory := filepath.Join(root, name)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	value := api.Configuration{Version: "email-bridge/v1", Revision: revision, ManagedBy: "git", Source: "fixture", Mailboxes: []api.Mailbox{}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "mailboxes.json"), raw, 0440); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(name, filepath.Join(root, "..next")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "..next"), filepath.Join(root, "..data")); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationPublicationRequiresWatermark(t *testing.T) {
	root := t.TempDir()
	var revision int64
	var digest string
	accept := func(_ context.Context, value api.Configuration, nextDigest string) error {
		if value.Revision < revision || value.Revision == revision && nextDigest != digest {
			return errors.New("configuration rollback")
		}
		revision, digest = value.Revision, nextDigest
		return nil
	}
	runtime := &configurationRuntime{root: root, accept: accept, build: func(snapshot *configuration.Snapshot) *mail.Service {
		return &mail.Service{Config: snapshot.Configuration}
	}}
	mountConfiguration(t, root, "..first", 1)
	if err := runtime.Refresh(t.Context()); err != nil || runtime.Service().Config.Revision != 1 {
		t.Fatal("initial configuration not published")
	}
	old := runtime.Service()
	mountConfiguration(t, root, "..second", 2)
	entered, proceed, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	runtime.accept = func(ctx context.Context, value api.Configuration, digest string) error {
		close(entered)
		select {
		case <-proceed:
			return accept(ctx, value, digest)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	go func() { finished <- runtime.Refresh(t.Context()) }()
	<-entered
	if runtime.Service() != old {
		t.Error("configuration published before watermark")
	}
	close(proceed)
	if err := <-finished; err != nil || runtime.Service().Config.Revision != 2 || old.Config.Revision != 1 {
		t.Fatal("atomic replacement failed")
	}
	runtime.accept = accept
	mountConfiguration(t, root, "..rollback", 1)
	if runtime.Refresh(t.Context()) == nil || runtime.Service() != nil || revision != 2 {
		t.Fatal("rollback did not fail closed")
	}
	restarted := &configurationRuntime{root: root, accept: accept, build: runtime.build}
	if restarted.Refresh(t.Context()) == nil || restarted.Service() != nil {
		t.Fatal("restart bypassed durable watermark")
	}
	mountConfiguration(t, root, "..recovered", 3)
	if err := runtime.Refresh(t.Context()); err != nil || runtime.Service().Config.Revision != 3 {
		t.Fatal("new revision did not recover")
	}
	if err := os.Remove(filepath.Join(root, "..data")); err != nil {
		t.Fatal(err)
	}
	if runtime.Refresh(t.Context()) == nil || runtime.Service() != nil {
		t.Fatal("missing projection retained active configuration")
	}
}

func TestConfigurationCancellationJoinsRefresh(t *testing.T) {
	root := t.TempDir()
	mountConfiguration(t, root, "..first", 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	runtime := &configurationRuntime{root: root, accept: func(ctx context.Context, _ api.Configuration, _ string) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}, build: func(*configuration.Snapshot) *mail.Service { panic("cancelled snapshot published") }}
	finished := make(chan error, 1)
	go func() { finished <- runtime.Refresh(ctx) }()
	<-entered
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) || runtime.Service() != nil {
		t.Fatal("cancelled refresh did not join")
	}
}

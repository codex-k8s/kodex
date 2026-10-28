package sshcompose

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fixtureTarget supplies documentation-only public addresses and a synthetic host pin.
// fixtureTarget задаёт документационные адреса и синтетический host pin.
func fixtureTarget() Target {
	key := append([]byte{0, 0, 0, 11}, []byte("ssh-ed25519")...)
	key = append(key, 0, 0, 0, 32)
	key = append(key, bytes.Repeat([]byte{1}, 32)...)
	return Target{Ref: "target_fixture", ProjectRef: "project_fixture", Repository: "example/project", Address: "192.0.2.10", Port: 22, User: "deployer", HostKey: "ssh-ed25519 " + base64.StdEncoding.EncodeToString(key), PublishedPort: 8080, RuntimeImage: "registry.example/app@sha256:" + strings.Repeat("a", 64)}
}

// fixtureBundle builds a complete tiny source tree without external Git or Docker.
// fixtureBundle создаёт полное небольшое дерево без внешнего Git либо Docker.
func fixtureBundle(t *testing.T) Bundle {
	t.Helper()
	manifest := Manifest{Image: "registry.example/app@sha256:" + strings.Repeat("a", 64), Command: []string{"python", "server.py"}, ContainerPort: 8080, HealthPath: "/health"}
	metadata, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files := []File{}
	for name, raw := range map[string][]byte{"kodex-deploy.json": metadata, "server.py": []byte("print('fixture')\n")} {
		files = append(files, File{Path: name, Mode: "100644", BlobSHA: GitObjectID("blob", raw), Content: base64.StdEncoding.EncodeToString(raw)})
	}
	return Bundle{Version: ProtocolVersion, Repository: "example/project", MergedSHA: strings.Repeat("b", 40), TreeSHA: TreeID(files), Files: SortedFiles(files)}
}

// fixturePermit binds the synthetic complete release to a short owner lease.
// fixturePermit связывает синтетический релиз с короткой owner lease.
func fixturePermit(t *testing.T, bundle Bundle) Permit {
	t.Helper()
	raw, _, err := bundle.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	return Permit{Version: ProtocolVersion, Purpose: "DEPLOY", Target: fixtureTarget(), InvocationRef: "invocation_fixture", DeploymentRef: "invocation_fixture", RootRunRef: "root_fixture", SessionRef: "session_fixture", TurnRef: "turn_fixture", Attempt: 1, LeaseRef: "lease_fixture", Generation: 1, EffectKey: "effect_fixture", InputDigest: strings.Repeat("c", 64), DeploymentInputDigest: strings.Repeat("c", 64), MergedSHA: bundle.MergedSHA, TreeSHA: bundle.TreeSHA, BundleSHA256: Digest(raw), IssuedAt: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(), Deadline: now.Add(MaximumLifetime).Unix()}
}

// TestOwnerPermitRejectsTamperingAndExpiry verifies cryptographic and exact target boundaries.
// TestOwnerPermitRejectsTamperingAndExpiry проверяет подпись, сроки и точную цель.
func TestOwnerPermitRejectsTamperingAndExpiry(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	permit := fixturePermit(t, fixtureBundle(t))
	signed, err := Sign(permit, private, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]ed25519.PublicKey{Digest(public): public}
	if decoded, err := Verify(signed, keys, time.Now()); err != nil || decoded.Target != permit.Target {
		t.Fatal("valid permit rejected")
	}
	for _, change := range []func(*SignedPermit){func(p *SignedPermit) { p.KeyID = "foreign" }, func(p *SignedPermit) { p.Payload = base64.StdEncoding.EncodeToString([]byte(`{"version":1}`)) }, func(p *SignedPermit) { p.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64)) }} {
		changed := signed
		change(&changed)
		if _, err := Verify(changed, keys, time.Now()); err == nil {
			t.Fatal("tampered permit accepted")
		}
	}
	if _, err := Verify(signed, keys, time.Now().Add(31*time.Second)); err == nil {
		t.Fatal("expired permit accepted")
	}
}

// TestReleaseRequiresCompleteTree rejects omitted, substituted, linked and traversing files.
// TestReleaseRequiresCompleteTree отклоняет пропущенные, подменённые файлы и traversal.
func TestReleaseRequiresCompleteTree(t *testing.T) {
	base := fixtureBundle(t)
	for name, change := range map[string]func(*Bundle){
		"omitted": func(b *Bundle) { b.Files = b.Files[:1] }, "bytes": func(b *Bundle) { b.Files[0].Content = base64.StdEncoding.EncodeToString([]byte("different")) },
		"symlink": func(b *Bundle) { b.Files[0].Mode = "120000" }, "traversal": func(b *Bundle) { b.Files[0].Path = "../escape" },
		"duplicate": func(b *Bundle) { b.Files = append(b.Files, b.Files[0]) }, "source-root": func(b *Bundle) { b.TreeSHA = strings.Repeat("d", 40) },
	} {
		t.Run(name, func(t *testing.T) {
			value := base
			value.Files = append([]File(nil), base.Files...)
			change(&value)
			if _, _, err := value.Canonical(); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
	if _, _, err := base.Canonical(); err != nil {
		t.Fatal(err)
	}
}

// TestReleaseFileCountBound accepts a complete template-sized tree and the exact
// cap, while rejecting an additional file without weakening byte or tree pins.
// TestReleaseFileCountBound допускает полное дерево размера шаблона и точный
// предел, отклоняя лишний файл без ослабления byte limits и привязки дерева.
func TestReleaseFileCountBound(t *testing.T) {
	for _, count := range []int{141, 256, 257} {
		t.Run(fmt.Sprintf("files-%d", count), func(t *testing.T) {
			bundle := fixtureBundle(t)
			for len(bundle.Files) < count {
				raw := bytes.Repeat([]byte("x"), 3300)
				bundle.Files = append(bundle.Files, File{Path: fmt.Sprintf("template/file-%03d.txt", len(bundle.Files)), Mode: "100644", BlobSHA: GitObjectID("blob", raw), Content: base64.StdEncoding.EncodeToString(raw)})
			}
			bundle.Files = SortedFiles(bundle.Files)
			bundle.TreeSHA = TreeID(bundle.Files)
			_, _, err := bundle.Canonical()
			if (err == nil) != (count <= 256) {
				t.Fatalf("complete tree file bound mismatch for %d files: %v", count, err)
			}
		})
	}
}

// TestStrictDecodeRejectsDuplicateUnknownAndTrailingJSON protects presemantic parsing.
// TestStrictDecodeRejectsDuplicateUnknownAndTrailingJSON защищает разбор до семантики.
func TestStrictDecodeRejectsDuplicateUnknownAndTrailingJSON(t *testing.T) {
	for _, raw := range []string{`{"version":1,"version":1}`, `{"version":1,"unknown":true}`, `{"version":1} {}`, `{"version":`} {
		var value struct {
			Version int `json:"version"`
		}
		if Decode([]byte(raw), &value, 4096) == nil {
			t.Fatal("ambiguous JSON accepted")
		}
	}
}

// TestComposeHasNoArbitraryHostMountOrBuild checks the materialized deployment profile.
// TestComposeHasNoArbitraryHostMountOrBuild проверяет итоговый профиль развёртывания.
func TestComposeHasNoArbitraryHostMountOrBuild(t *testing.T) {
	_, manifest, err := fixtureBundle(t).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Compose(manifest, fixtureTarget(), "/home/deployer/releases/exact/source")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if json.Unmarshal(raw, &document) != nil {
		t.Fatal("invalid Compose")
	}
	service := document["services"].(map[string]any)["app"].(map[string]any)
	if service["read_only"] != true || service["build"] != nil || service["privileged"] != nil || service["network_mode"] != nil {
		t.Fatal("unsafe Compose profile")
	}
	manifest.Command = []string{"${HOST_SECRET}"}
	if _, err := Compose(manifest, fixtureTarget(), "/home/deployer/source"); err == nil {
		t.Fatal("host environment interpolation accepted")
	}
}

// TestJournalRejectsReplayMutationAndStaleRenew preserves one winner across reopen.
// TestJournalRejectsReplayMutationAndStaleRenew сохраняет одного победителя после reopen.
func TestJournalRejectsReplayMutationAndStaleRenew(t *testing.T) {
	root := filepath.Join(t.TempDir(), "journal")
	journal, err := OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	permit := fixturePermit(t, fixtureBundle(t))
	receipt, fresh, err := journal.Reserve(permit, "fixture")
	if err != nil || !fresh {
		t.Fatal("initial reservation failed")
	}
	if _, fresh, err := journal.Reserve(permit, "fixture"); err != nil || fresh {
		t.Fatal("duplicate effect restarted")
	}
	changed := permit
	changed.DeploymentInputDigest = strings.Repeat("f", 64)
	if _, _, err := journal.Reserve(changed, "fixture"); err == nil {
		t.Fatal("effect input substitution accepted")
	}
	renewal := permit
	renewal.Purpose = "RENEW"
	renewal.Generation++
	if _, err := journal.Renew(receipt, renewal); err == nil {
		t.Fatal("another generation renewed old process")
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err = OpenJournal(root)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if restored, err := journal.Read(permit.DeploymentRef); err != nil || !restored.Matches(permit) {
		t.Fatal("durable receipt lost")
	}
	if _, fresh, err := journal.Reserve(permit, "fixture"); err != nil || fresh {
		t.Fatal("reopen replay started effect")
	}
}

// fixtureRunner records only helper-owned argv and supports a cancellable provider delay.
// fixtureRunner учитывает только helper argv и поддерживает отменяемую задержку.
type fixtureRunner struct {
	calls   atomic.Int32
	wait    bool
	started chan struct{}
}

// Rootless supplies the fixture admission without contacting the Docker daemon.
// Rootless предоставляет fixture admission без обращения к Docker daemon.
func (*fixtureRunner) Rootless(context.Context) bool { return true }

// Run models either a finished effect or a process waiting for exact cancellation.
// Run моделирует законченный эффект либо процесс, ожидающий точную отмену.
func (runner *fixtureRunner) Run(ctx context.Context, args []string) error {
	runner.calls.Add(1)
	if runner.started != nil {
		close(runner.started)
	}
	if runner.wait {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

// TestHelperStopsExactAttemptAndNeverReplaysAcrossInvocationRefs checks EOF, revoke, stale renew and expiry.
// TestHelperStopsExactAttemptAndNeverReplaysAcrossInvocationRefs проверяет EOF, revoke, stale renew и expiry.
func TestHelperStopsExactAttemptAndNeverReplaysAcrossInvocationRefs(t *testing.T) {
	for _, failure := range []string{"eof", "cancel", "stale-renew", "expiry"} {
		t.Run(failure, func(t *testing.T) {
			public, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			bundle := fixtureBundle(t)
			permit := fixturePermit(t, bundle)
			if failure == "expiry" {
				permit.ExpiresAt = time.Now().Add(time.Second).Unix()
			}
			signed, err := Sign(permit, private, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			runner := &fixtureRunner{wait: true, started: make(chan struct{})}
			helper := Helper{Target: permit.Target, OwnerKeys: map[string]ed25519.PublicKey{Digest(public): public}, RootDirectory: t.TempDir(), Runner: runner}
			reader, writer := io.Pipe()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var output bytes.Buffer
			done := make(chan error, 1)
			go func() { done <- helper.Serve(ctx, reader, &output) }()
			if json.NewEncoder(writer).Encode(Request{Permit: signed, Bundle: &bundle}) != nil {
				t.Fatal("initial frame rejected")
			}
			select {
			case <-runner.started:
			case <-time.After(time.Second):
				t.Fatal("effect did not start")
			}
			status := permit
			status.Purpose = "STATUS"
			statusPermit, err := Sign(status, private, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(Request{Permit: statusPermit})
			var observed bytes.Buffer
			if helper.Serve(ctx, io.NopCloser(bytes.NewReader(append(raw, '\n'))), &observed) != nil {
				t.Fatal("active receipt read blocked on writer")
			}
			switch failure {
			case "eof":
				_ = writer.Close()
			case "cancel":
				cancel()
			case "stale-renew":
				renew := permit
				renew.Purpose = "RENEW"
				renew.Generation++
				frame, err := Sign(renew, private, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				if json.NewEncoder(writer).Encode(Request{Permit: frame}) != nil {
					t.Fatal("renew frame rejected before verification")
				}
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("attempt or reader did not join")
			}
			_ = writer.Close()
			var response Response
			if Decode(bytes.TrimSpace(output.Bytes()), &response, 16384) != nil || response.Receipt == nil || response.Receipt.State != "UNKNOWN_OUTCOME" || response.Receipt.HealthOK {
				t.Fatal("cancelled effect falsely completed")
			}
			permit.InvocationRef = "invocation_replay"
			permit.DeploymentRef = permit.InvocationRef
			permit.IssuedAt = time.Now().Unix()
			permit.ExpiresAt = time.Now().Add(30 * time.Second).Unix()
			replay, err := Sign(permit, private, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			raw, _ = json.Marshal(Request{Permit: replay, Bundle: &bundle})
			if helper.Serve(context.Background(), io.NopCloser(bytes.NewReader(append(raw, '\n'))), io.Discard) != nil {
				t.Fatal("duplicate effect receipt was not observable")
			}
			if runner.calls.Load() != 1 {
				t.Fatal("new invocation replayed the same release")
			}
		})
	}
}

// TestHelperUsesOneEffectAndReadOnlyRecovery exercises signed stdin and durable extraction.
// TestHelperUsesOneEffectAndReadOnlyRecovery проверяет signed stdin и durable extraction.
func TestHelperUsesOneEffectAndReadOnlyRecovery(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bundle := fixtureBundle(t)
	permit := fixturePermit(t, bundle)
	runner := &fixtureRunner{}
	helper := Helper{Target: permit.Target, OwnerKeys: map[string]ed25519.PublicKey{Digest(public): public}, RootDirectory: t.TempDir(), Runner: runner, Health: func(context.Context, int, string) bool { return true }}
	signed, err := Sign(permit, private, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	var result bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- helper.Serve(context.Background(), reader, &result) }()
	if err := json.NewEncoder(writer).Encode(Request{Permit: signed, Bundle: &bundle}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	var response Response
	if Decode(bytes.TrimSpace(result.Bytes()), &response, 16384) != nil || response.Receipt == nil || response.Receipt.State != "SUCCEEDED" || !response.Receipt.HealthOK {
		t.Fatal("healthy deployment not proven")
	}
	if runner.calls.Load() != 1 {
		t.Fatal("unexpected provider effect count")
	}
	permit.Purpose = "STATUS"
	signed, err = Sign(permit, private, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(Request{Permit: signed})
	result.Reset()
	if err := helper.Serve(context.Background(), io.NopCloser(bytes.NewReader(append(request, '\n'))), &result); err != nil {
		t.Fatal(err)
	}
	if runner.calls.Load() != 1 {
		t.Fatal("status repeated deployment")
	}
	if _, err := os.Stat(filepath.Join(helper.RootDirectory, "releases", permit.BundleSHA256, "source", "server.py")); err != nil {
		t.Fatal("source missing")
	}
}

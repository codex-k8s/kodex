package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func TestValidArtifactOutputPathRejectsTraversalAndCredentialNames(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"../result.txt", "/tmp/result.txt", `dir\\result.txt`, "dir/../result.txt", ".env", "credentials.json", "nested/auth.json", "bad\x00name", "result\u202etxt"} {
		if validArtifactOutputPath(value) {
			t.Errorf("validArtifactOutputPath(%q) = true", value)
		}
	}
	for _, value := range []string{"result.txt", "images/result.png", "отчёт/результат.pdf"} {
		if !validArtifactOutputPath(value) {
			t.Errorf("validArtifactOutputPath(%q) = false", value)
		}
	}
}

func TestArtifactBridgeOpenat2RejectsSpecialFilesAndKeepsOpenedInode(t *testing.T) {
	outbox := t.TempDir()
	original := filepath.Join(outbox, "result.txt")
	if err := os.WriteFile(original, []byte("original result"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("result.txt", filepath.Join(outbox, "symlink.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, filepath.Join(outbox, "hardlink.txt")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(outbox, "pipe.txt"), 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var published string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/artifacts/publish") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		published = string(body)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(artifactPublishResponse{ArtifactVersionID: strings.Repeat("a", 32), DeliveryID: strings.Repeat("b", 32), State: "delivered"})
	}))
	defer server.Close()

	r := &runner{secrets: secretInventory{values: map[string]struct{}{}, sources: map[string]int64{}}}
	bridge := &artifactBridge{runner: r, active: &activeArtifactTurn{
		botServiceURL: server.URL, sessionKey: "session-1", sessionToken: "session-token", turnID: "run-1", outboxDir: outbox,
	}}
	for _, path := range []string{"symlink.txt", "hardlink.txt", "pipe.txt"} {
		if _, err := bridge.publish(context.Background(), publishArtifactToolInput{Path: path, IdempotencyKey: "reject-" + path}); err == nil {
			t.Fatalf("publish(%q) unexpectedly succeeded", path)
		}
	}
	if err := os.Remove(filepath.Join(outbox, "hardlink.txt")); err != nil {
		t.Fatal(err)
	}
	bridge.openHook = func() {
		bridge.openHook = nil
		if err := os.Rename(original, filepath.Join(outbox, "opened-original.txt")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(original, []byte("replacement attack"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := bridge.publish(context.Background(), publishArtifactToolInput{Path: "result.txt", IdempotencyKey: "race-proof"}); err != nil {
		t.Fatalf("publish race proof error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if published != "original result" {
		t.Fatalf("published body = %q, want opened inode body", published)
	}
}

func TestArtifactBridgeRejectsSymlinkedOutboxRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	realOutbox := filepath.Join(root, "real-outbox")
	if err := os.Mkdir(realOutbox, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realOutbox, "result.txt"), []byte("must not publish"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedOutbox := filepath.Join(root, "linked-outbox")
	if err := os.Symlink(realOutbox, linkedOutbox); err != nil {
		t.Fatal(err)
	}
	bridge := &artifactBridge{runner: &runner{}, active: &activeArtifactTurn{outboxDir: linkedOutbox}}
	if _, err := bridge.publish(context.Background(), publishArtifactToolInput{Path: "result.txt", IdempotencyKey: "linked-root"}); err == nil {
		t.Fatal("симлинк корня outbox не был отклонён")
	}
}

func TestArtifactBridgeQuarantinesKnownSecretWithoutUploadingBody(t *testing.T) {
	t.Parallel()
	outbox := t.TempDir()
	secretValue := "synthetic-secret-value-123456789"
	if err := os.WriteFile(filepath.Join(outbox, "result.txt"), []byte("value="+secretValue), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := compileSecretInventory(map[string]struct{}{secretValue: {}}, map[string]int64{}, map[string]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	var publishCalled bool
	var quarantine artifactQuarantineRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/artifacts/publish") {
			publishCalled = true
			http.Error(w, "unexpected upload", http.StatusInternalServerError)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/artifacts/quarantine") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&quarantine)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(artifactPublishResponse{ArtifactVersionID: strings.Repeat("a", 32), DeliveryID: strings.Repeat("b", 32), State: "quarantined", Quarantined: true})
	}))
	defer server.Close()
	bridge := &artifactBridge{runner: &runner{secrets: inventory}, active: &activeArtifactTurn{
		botServiceURL: server.URL, sessionKey: "session-1", sessionToken: "session-token", turnID: "run-1", outboxDir: outbox,
	}}
	result, err := bridge.publish(context.Background(), publishArtifactToolInput{Path: "result.txt", IdempotencyKey: "secret-output"})
	if err != nil || !result.Quarantined || publishCalled || quarantine.Reason != "secret_detected" || strings.Contains(quarantine.OriginalName, secretValue) {
		t.Fatalf("quarantine result=%#v request=%#v publishCalled=%t error=%v", result, quarantine, publishCalled, err)
	}
}

func TestPrepareTurnArtifactsVerifiesManifestAndCreatesReadOnlyInbox(t *testing.T) {
	body := []byte("attachment body")
	digest := sha256.Sum256(body)
	hashText := hex.EncodeToString(digest[:])
	versionID := strings.Repeat("a", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer session-token" || r.URL.Query().Get("turn_id") != "run-1" {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Length", "15")
		w.Header().Set("X-MatterCodex-Artifact-SHA256", hashText)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	oldWorkspace := workspaceDir
	workspaceDir = t.TempDir()
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Join(workspaceDir, ".matter-codex", "inbox", "run-1"), 0o700)
		workspaceDir = oldWorkspace
	})
	r := &runner{}
	claim := sessionTurnClaimResponse{
		RunID: "run-1",
		ArtifactManifest: sessionArtifactManifest{SchemaVersion: artifactManifestSchema, TurnID: "run-1", Files: []sessionArtifactManifestEntry{{
			OriginalName: "unsafe-name.txt", LocalPath: "/workspace/.matter-codex/inbox/run-1/1-" + versionID + ".txt",
			MediaType: "text/plain", Size: int64(len(body)), SHA256: hashText, ArtifactVersionID: versionID,
			Source: sessionArtifactManifestSource{Kind: "mattermost", PostID: "post-1", FileID: "file-1"},
		}}},
	}
	outbox, err := r.prepareTurnArtifacts(context.Background(), server.Client(), server.URL, "session-1", "session-token", claim)
	if err != nil {
		t.Fatalf("prepareTurnArtifacts() error = %v", err)
	}
	if info, err := os.Stat(outbox); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("outbox mode=%v error=%v", info, err)
	}
	inbox := filepath.Join(workspaceDir, ".matter-codex", "inbox", "run-1")
	filePath := filepath.Join(inbox, "1-"+versionID+".txt")
	read, err := os.ReadFile(filePath)
	if err != nil || string(read) != string(body) {
		t.Fatalf("inbox body=%q error=%v", read, err)
	}
	if info, err := os.Stat(filePath); err != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("inbox file mode=%v error=%v", info, err)
	}
	if info, err := os.Stat(filepath.Join(inbox, "manifest.json")); err != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("manifest mode=%v error=%v", info, err)
	}
	manifestBody, err := os.ReadFile(filepath.Join(inbox, "manifest.json"))
	if err != nil || !bytes.Contains(manifestBody, []byte(`"post_id": "post-1"`)) || !bytes.Contains(manifestBody, []byte(`"file_id": "file-1"`)) {
		t.Fatalf("manifest source lost: body=%s error=%v", manifestBody, err)
	}
}

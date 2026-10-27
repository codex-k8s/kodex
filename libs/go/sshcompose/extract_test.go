package sshcompose

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestExtractBundlePreservesExecutableModeAndRejectsReplayDrift exercises real private files
// under a restrictive umask. A Git executable remains executable, ordinary content stays nonexecutable,
// and immutable replay rejects mode substitution instead of silently changing existing permissions.
// TestExtractBundlePreservesExecutableModeAndRejectsReplayDrift проверяет настоящие private files
// при строгом umask. Git executable сохраняет запуск, обычный файл не получает execute,
// а immutable replay отклоняет подмену mode без скрытого изменения существующих прав.
func TestExtractBundlePreservesExecutableModeAndRejectsReplayDrift(t *testing.T) {
	previousMask := syscall.Umask(0077)
	defer syscall.Umask(previousMask)
	bundle := fixtureBundle(t)
	manifest := Manifest{Image: fixtureTarget().RuntimeImage, Command: []string{"/app/start.sh"}, ContainerPort: 8080, HealthPath: "/health"}
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for index := range bundle.Files {
		if bundle.Files[index].Path == "kodex-deploy.json" {
			bundle.Files[index].Content = base64.StdEncoding.EncodeToString(rawManifest)
			bundle.Files[index].BlobSHA = GitObjectID("blob", rawManifest)
		}
	}
	script := []byte("#!/bin/sh\nprintf 'mode-fixture\\n'\n")
	bundle.Files = append(bundle.Files, File{Path: "start.sh", Mode: "100755", BlobSHA: GitObjectID("blob", script), Content: base64.StdEncoding.EncodeToString(script)})
	bundle.Files = SortedFiles(bundle.Files)
	bundle.TreeSHA = TreeID(bundle.Files)
	if _, _, err := bundle.Canonical(); err != nil {
		t.Fatal("valid executable release rejected")
	}
	release := filepath.Join(t.TempDir(), "release")
	if err := extractBundle(release, bundle); err != nil {
		t.Fatal(err)
	}
	for _, file := range bundle.Files {
		want := os.FileMode(0644)
		if file.Mode == "100755" {
			want = 0755
		}
		info, err := os.Stat(filepath.Join(release, "source", file.Path))
		if err != nil || info.Mode() != want {
			t.Fatal("extracted Git mode differs from the committed release")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, filepath.Join(release, "source", "start.sh")).CombinedOutput()
	if err != nil || string(output) != "mode-fixture\n" {
		t.Fatal("executable source did not run as an exact argument vector")
	}
	if err := extractBundle(release, bundle); err != nil {
		t.Fatal("same-byte same-mode replay rejected")
	}
	for _, file := range []string{"start.sh", "server.py"} {
		name := filepath.Join(release, "source", file)
		original, changed := os.FileMode(0755), os.FileMode(0644)
		if file == "server.py" {
			original, changed = 0644, 0755
		}
		if err := os.Chmod(name, changed); err != nil {
			t.Fatal(err)
		}
		if err := extractBundle(release, bundle); err == nil {
			t.Fatal("immutable replay accepted mode substitution")
		}
		if err := os.Chmod(name, original); err != nil {
			t.Fatal(err)
		}
	}
}

package configuration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

func fixture(t *testing.T) api.Configuration {
	t.Helper()
	raw, err := os.ReadFile("../../../../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var value api.Configuration
	if api.Decode(raw, &value) != nil {
		t.Fatal("invalid configuration fixture")
	}
	return value
}

func writeGeneration(t *testing.T, root, name string, value api.Configuration) string {
	t.Helper()
	directory := filepath.Join(root, name)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for key, contents := range map[string][]byte{"mailboxes.json": raw, "ca.1": []byte("fixture"), "user.1": []byte("fixture"), "password.1": []byte(strconv.FormatInt(value.Revision, 10))} {
		if err := os.WriteFile(filepath.Join(directory, key), contents, 0440); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func switchGeneration(root, name string) error {
	next := filepath.Join(root, "..next")
	if err := os.Symlink(name, next); err != nil {
		return err
	}
	return os.Rename(next, filepath.Join(root, "..data"))
}

func TestSnapshotPinsAtomicGeneration(t *testing.T) {
	root := t.TempDir()
	value := fixture(t)
	firstDirectory := writeGeneration(t, root, "..first", value)
	if err := switchGeneration(root, "..first"); err != nil {
		t.Fatal(err)
	}
	first, err := Load(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	value.Revision = 2
	writeGeneration(t, root, "..second", value)
	if err := switchGeneration(root, "..second"); err != nil {
		t.Fatal(err)
	}
	second, err := Load(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(firstDirectory); err != nil {
		t.Fatal(err)
	}
	descriptor := api.Descriptor{Name: "password", Generation: 1}
	for _, snapshot := range []*Snapshot{first, second} {
		secret, err := snapshot.Read(t.Context(), descriptor)
		if err != nil || string(secret) != strconv.FormatInt(snapshot.Configuration.Revision, 10) {
			t.Fatal("snapshot mixed configuration and credential generations")
		}
		secret[0] = 'x'
		again, _ := snapshot.Read(t.Context(), descriptor)
		if again[0] == 'x' {
			t.Fatal("caller mutated immutable credentials")
		}
	}
	if _, err := first.Read(t.Context(), api.Descriptor{Name: "password", Generation: 2}); err == nil {
		t.Fatal("foreign descriptor generation accepted")
	}
}

func TestSnapshotConcurrentMountRotation(t *testing.T) {
	root := t.TempDir()
	value := fixture(t)
	writeGeneration(t, root, "..first", value)
	value.Revision = 2
	writeGeneration(t, root, "..second", value)
	if err := switchGeneration(root, "..first"); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	group.Go(func() {
		for range 40 {
			for _, name := range []string{"..first", "..second"} {
				if err := switchGeneration(root, name); err != nil {
					t.Error(err)
					return
				}
			}
		}
	})
	for range 40 {
		snapshot, err := Load(t.Context(), root)
		if err != nil {
			t.Error(err)
			break
		}
		secret, err := snapshot.Read(t.Context(), api.Descriptor{Name: "password", Generation: 1})
		if err != nil || string(secret) != strconv.FormatInt(snapshot.Configuration.Revision, 10) {
			t.Error("rotation mixed credential generations")
			break
		}
	}
	group.Wait()
}

func TestSnapshotRejectsUnsafeProjection(t *testing.T) {
	for _, name := range []string{"missing-link", "escaped-generation", "missing-key", "empty-key", "writable-key", "escaped-key", "invalid-document", "oversized-key", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			directory := writeGeneration(t, root, "..first", fixture(t))
			destination := "..first"
			if name == "escaped-generation" {
				destination = writeGeneration(t, t.TempDir(), "..outside", fixture(t))
			}
			if name != "missing-link" {
				if err := switchGeneration(root, destination); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(directory, "password.1")
			switch name {
			case "missing-key", "empty-key", "writable-key", "escaped-key", "oversized-key":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				var err error
				switch name {
				case "empty-key":
					err = os.WriteFile(path, nil, 0440)
				case "writable-key":
					err = os.WriteFile(path, []byte("fixture"), 0600)
				case "escaped-key":
					outside := filepath.Join(t.TempDir(), "password.1")
					if err := os.WriteFile(outside, []byte("fixture"), 0440); err != nil {
						t.Fatal(err)
					}
					err = os.Symlink(outside, path)
				case "oversized-key":
					err = os.WriteFile(path, []byte(strings.Repeat("x", maximumBytes)), 0440)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "invalid-document":
				path = filepath.Join(directory, "mailboxes.json")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(`{"unknown":true}`), 0440); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if name == "cancelled" {
				cancel()
			}
			if _, err := Load(ctx, root); err == nil {
				t.Fatal("unsafe projection accepted")
			}
		})
	}
}

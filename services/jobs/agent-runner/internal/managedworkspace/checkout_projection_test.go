package managedworkspace

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCheckoutFiveFileNestedProjection exercises full gateway-shaped commit/tree/file projections
// and atomic filesystem publication with the runtime process's actual UID/GID. All bytes and IDs
// are synthetic; this does not prove a live provider or a bound V2 mount.
// TestCheckoutFiveFileNestedProjection проверяет полные gateway-shaped commit/tree/file projections
// и атомарную публикацию с фактическими UID/GID процесса. Bytes и IDs синтетические; тест не
// подтверждает живой provider либо привязанный V2 mount.
func TestCheckoutFiveFileNestedProjection(t *testing.T) {
	handler, request := snapshotFixture(t)
	original := handler.owner
	files := map[string]string{
		".gitlab-ci.yml":         "stages: [test]\n",
		"go.mod":                 "module example.invalid/repository\n\ngo 1.26\n",
		"main.go":                "package main\nfunc main() {}\n",
		"main_test.go":           "package main\n",
		"markers/acceptance.txt": "Synthetic managed snapshot check.\n",
	}
	blobs := map[string]string{}
	for name, text := range files {
		object := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(text))), []byte(text)...))
		blobs[name] = hex.EncodeToString(object[:])
	}
	calls := 0
	handler.owner = func(ctx context.Context, id, tool string, args map[string]any) (map[string]any, error) {
		calls++
		fields := args["input"].(map[string]any)
		if fields["ref"] != request.SHA {
			t.Fatal("unpinned commit")
		}
		var projection any
		switch args["grant_ref"] {
		case "grt_fixture_0":
			if _, err := original(ctx, id, tool, args); err != nil {
				t.Fatal(err)
			}
			projection = map[string]any{"id": request.SHA, "short_id": request.SHA[:8], "title": "Synthetic commit", "message": "Synthetic commit\n", "web_url": "https://gitlab.example.invalid/example/repository/-/commit/" + request.SHA}
		case "grt_fixture_1":
			directory, _ := fields["path"].(string)
			entries := []map[string]string{}
			if directory == "" {
				entries = append(entries, map[string]string{"id": strings.Repeat("b", 40), "name": "markers", "path": "markers", "type": "tree", "mode": "040000"})
			}
			for name := range files {
				if path.Dir(name) == directoryParent(directory) {
					entries = append(entries, map[string]string{"id": blobs[name], "name": path.Base(name), "path": name, "type": "blob", "mode": "100644"})
				}
			}
			sort.Slice(entries, func(i, j int) bool { return entries[i]["path"] < entries[j]["path"] })
			raw, _ := json.Marshal(entries)
			projection = map[string]any{"items": string(raw), "count": len(entries)}
		case "grt_fixture_2":
			name, _ := fields["file_path"].(string)
			text, ok := files[name]
			if !ok {
				t.Fatal("unknown file")
			}
			projection = map[string]any{"file_path": name, "ref": request.SHA, "blob_id": blobs[name], "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(text))}
		default:
			t.Fatal("unexpected grant")
		}
		raw, err := json.Marshal(projection)
		if err != nil {
			t.Fatal(err)
		}
		return map[string]any{"ok": true, "invocationRef": "inv_synthetic", "result": string(raw)}, nil
	}
	raw, _ := json.Marshal(request)
	result, err := handler.checkout(t.Context(), "nested-checkout", raw)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 8 || result.(map[string]any)["files"] != len(files) {
		t.Fatal("incomplete snapshot result", calls)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(handler.store.root, request.Destination, name))
		if err != nil || string(got) != want {
			t.Fatal("snapshot bytes differ", name, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(handler.store.root, "work"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "repository" {
		t.Fatal("unexpected staging residue", entries, err)
	}
}

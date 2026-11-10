package managedworkspace

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// snapshotFixture emulates only read-only managed gateway JSON at a synthetic exact commit.
// It is not evidence of live GitLab authorization, credentials, pagination or an installed image.
// snapshotFixture эмулирует лишь read-only managed gateway JSON на synthetic exact commit.
// Это не доказательство live GitLab authorization, credentials, pagination либо installed image.
func snapshotFixture(t *testing.T) (*Handler, checkoutRequest) {
	t.Helper()
	store := fileStoreFixture(t)
	sha := strings.Repeat("a", 40)
	content := []byte("stages: [test]\n")
	object := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(content))), content...))
	blob := hex.EncodeToString(object[:])
	request := checkoutRequest{Connection: "int_fixture", ProjectID: 1, ProjectPath: "example/repository", SHA: sha, Destination: "work/repository"}
	for index, operation := range []string{"gitlab.commit.read", "gitlab.repository.tree.list", "gitlab.repository.file.read"} {
		store.input.IntegrationGrants = append(store.input.IntegrationGrants, runtimecontract.RunnerIntegrationGrant{Ref: fmt.Sprintf("grt_fixture_%d", index), ConnectionRef: request.Connection, ConnectionVersion: 1, GrantVersion: 1, DefinitionKey: "gitlab", DefinitionDigest: strings.Repeat("b", 64), Risk: "READ", Operation: operation, CapabilityKey: operation})
	}
	handler := &Handler{store: store, owner: func(_ context.Context, _, name string, args map[string]any) (map[string]any, error) {
		if name != "invoke_integration" {
			t.Fatal("unexpected owner tool", name)
		}
		fields := args["input"].(map[string]any)
		if fields["ref"] != sha || fields["project_id"] != int64(1) || fields["project_path"] != request.ProjectPath {
			t.Fatal("unpinned repository read", fields)
		}
		var output any
		switch args["grant_ref"] {
		case "grt_fixture_0":
			output = map[string]any{"id": sha}
		case "grt_fixture_1":
			items, _ := json.Marshal([]map[string]string{{"id": blob, "path": ".gitlab-ci.yml", "name": ".gitlab-ci.yml", "type": "blob", "mode": "100644"}})
			output = map[string]any{"items": string(items), "count": 1}
		case "grt_fixture_2":
			output = map[string]any{"file_path": ".gitlab-ci.yml", "ref": sha, "blob_id": blob, "encoding": "base64", "content": base64.StdEncoding.EncodeToString(content)}
		default:
			t.Fatal("unknown read grant")
		}
		raw, _ := json.Marshal(output)
		return map[string]any{"ok": true, "result": string(raw)}, nil
	}}
	return handler, request
}

// TestCheckoutPublishesOnlyCompleteExactSnapshot exercises managed read selection, object hash,
// atomic install and refusal to overwrite an existing local working copy.
// TestCheckoutPublishesOnlyCompleteExactSnapshot проверяет managed read selection, object hash,
// atomic install и отказ перезаписи существующей локальной рабочей копии.
func TestCheckoutPublishesOnlyCompleteExactSnapshot(t *testing.T) {
	handler, request := snapshotFixture(t)
	raw, _ := json.Marshal(request)
	result, err := handler.checkout(context.Background(), "checkout-test", raw)
	if err != nil {
		t.Fatal(err)
	}
	value := result.(map[string]any)
	if value["representation"] != "API_SNAPSHOT" || value["source_commit_sha"] != request.SHA || value["files"] != 1 {
		t.Fatal(value)
	}
	data, err := os.ReadFile(filepath.Join(handler.store.root, "work/repository/.gitlab-ci.yml"))
	if err != nil || string(data) != "stages: [test]\n" {
		t.Fatal(string(data), err)
	}
	if _, err := os.Stat(filepath.Join(handler.store.root, "work/repository/.git")); !os.IsNotExist(err) {
		t.Fatal("snapshot fabricated git HEAD")
	}
	if _, err := handler.checkout(context.Background(), "checkout-again", raw); err == nil {
		t.Fatal("existing working copy replaced")
	}
	entries, err := os.ReadDir(filepath.Join(handler.store.root, "work"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "repository" {
		t.Fatal("partial staging retained", entries, err)
	}
}

// TestCheckoutRejectsChangedBlobAndIncompleteAuthority ensures neither bad remote bytes nor an
// incomplete cross-connection grant set can publish a misleading partial destination.
// TestCheckoutRejectsChangedBlobAndIncompleteAuthority гарантирует, что bad remote bytes либо
// incomplete cross-connection grants не публикуют ложный partial destination.
func TestCheckoutRejectsChangedBlobAndIncompleteAuthority(t *testing.T) {
	handler, request := snapshotFixture(t)
	owner := handler.owner
	handler.owner = func(ctx context.Context, id, name string, args map[string]any) (map[string]any, error) {
		value, err := owner(ctx, id, name, args)
		if args["grant_ref"] == "grt_fixture_2" {
			var data map[string]any
			_ = json.Unmarshal([]byte(value["result"].(string)), &data)
			data["content"] = base64.StdEncoding.EncodeToString([]byte("wrong bytes"))
			raw, _ := json.Marshal(data)
			value["result"] = string(raw)
		}
		return value, err
	}
	raw, _ := json.Marshal(request)
	if _, err := handler.checkout(context.Background(), "checkout-corrupt", raw); err == nil {
		t.Fatal("wrong Git blob hash accepted")
	}
	if _, err := os.Stat(filepath.Join(handler.store.root, request.Destination)); !os.IsNotExist(err) {
		t.Fatal("partial snapshot published")
	}
	handler.store.input.IntegrationGrants[2].ConnectionRef = "int_other"
	if _, err := checkoutGrants(handler.store.input, request.Connection); err == nil {
		t.Fatal("cross-connection authority accepted")
	}
	for _, name := range []string{"../outside", "a/../../outside", "/absolute", ".git/config", "a/.git/config", "a\\b", "a//b"} {
		if safeRepositoryPath(name) {
			t.Fatal("unsafe repository path accepted", name)
		}
	}
}

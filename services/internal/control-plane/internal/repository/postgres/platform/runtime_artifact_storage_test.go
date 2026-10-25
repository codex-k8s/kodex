package platform

import (
	"context"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// TestResultContainsPreparedObjects checks that a prepared object key matches its committed
// artifact ref and a different created ref cannot suppress cleanup. This pure predicate regression
// uses synthetic receipts and does not prove owner commit or object existence.
// TestResultContainsPreparedObjects проверяет совпадение prepared object key с committed artifact
// ref и невозможность подавить cleanup другим created ref. Pure predicate regression использует
// synthetic receipts и не доказывает owner commit либо object existence.
func TestResultContainsPreparedObjects(t *testing.T) {
	t.Parallel()
	digest := "sha256:" + strings.Repeat("a", 64)
	prepared := []objectstorage.Receipt{{
		Key: artifactObjectKey("org-1", "subject-1", "project-1", "art-1", digest), Digest: digest,
	}}
	if !resultContainsPreparedObjects(command.Result{CreatedRefs: []string{"art-1"}}, prepared) {
		t.Fatal("committed artifact was not recognized")
	}
	if resultContainsPreparedObjects(command.Result{CreatedRefs: []string{"art-2"}}, prepared) {
		t.Fatal("different artifact suppressed object cleanup")
	}
}

// TestCleanupPreparedObjects writes one synthetic object to the in-memory store, invokes
// uncommitted cleanup with an uncancelled context and requires exact-version Head to report
// NotFound. It exercises local cleanup behavior, not remote storage consistency or live deletion
// authority.
// TestCleanupPreparedObjects записывает synthetic object в in-memory store, вызывает uncommitted
// cleanup с uncancelled context и требует NotFound от exact-version Head. Проверяет local cleanup
// behavior, а не remote storage consistency либо live deletion authority.
func TestCleanupPreparedObjects(t *testing.T) {
	t.Parallel()
	store := objectstoragetest.New()
	digest := "sha256:" + strings.Repeat("b", 64)
	receipt, err := store.Put(t.Context(), objectstorage.PutInput{
		Key:       "organizations/org/projects/project/artifacts/art/digest",
		MediaType: "text/plain", Digest: digest, SizeBytes: 4,
		Body: strings.NewReader("test"),
	})
	if err != nil {
		t.Fatalf("put fixture: %v", err)
	}
	repository := &Repository{objects: store}
	repository.cleanupPreparedObjects(context.WithoutCancel(t.Context()), []objectstorage.Receipt{receipt}, false)
	if _, err := store.Head(t.Context(), receipt.Key, receipt.VersionID); err != objectstorage.ErrNotFound {
		t.Fatalf("prepared object was not deleted: %v", err)
	}
}

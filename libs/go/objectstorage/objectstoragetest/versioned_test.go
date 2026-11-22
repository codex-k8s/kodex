package objectstoragetest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// TestVersionedFixturePreservesExactVersionsAndDeletion proves that newer writes, missing
// versions and exact deletion cannot silently replace a caller's immutable accepted body.
// TestVersionedFixturePreservesExactVersionsAndDeletion проверяет, что new writes, missing
// versions и exact deletion не заменяют immutable accepted body вызывающей стороны.
func TestVersionedFixturePreservesExactVersionsAndDeletion(t *testing.T) {
	store := NewVersioned()
	// put owns the disposable source and checks the fixture's real byte commitment.
	// put владеет disposable source и проверяет actual byte commitment fixture.
	put := func(body string) objectstorage.Receipt {
		t.Helper()
		payload := []byte(body)
		digest := sha256.Sum256(payload)
		receipt, err := store.Put(t.Context(), objectstorage.PutInput{Key: "fixture/versioned", MediaType: "application/octet-stream", SizeBytes: int64(len(payload)), Digest: "sha256:" + hex.EncodeToString(digest[:]), Body: bytes.NewReader(payload)})
		clear(payload)
		if err != nil {
			t.Fatal(err)
		}
		return receipt
	}
	first := put("first body")
	second := put("second body")
	if first.VersionID == second.VersionID || store.CheckVersioning(t.Context()) != nil {
		t.Fatal("versioned fixture reused an immutable version")
	}
	for _, selected := range []struct {
		version, body string
		receipt       objectstorage.Receipt
	}{{first.VersionID, "first body", first}, {second.VersionID, "second body", second}, {"", "second body", second}} {
		object, err := store.Get(t.Context(), first.Key, selected.version)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(object.Body)
		_ = object.Body.Close()
		if err != nil || object.Receipt != selected.receipt || string(body) != selected.body {
			t.Fatal("exact version changed after another PUT", err)
		}
		clear(body)
	}
	if _, err := store.Get(t.Context(), first.Key, "missing"); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("missing version fell back to latest", err)
	}
	if err := store.Delete(t.Context(), first.Key, "missing"); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("missing version produced deletion evidence", err)
	}
	if err := store.Delete(t.Context(), first.Key, first.VersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Head(t.Context(), first.Key, first.VersionID); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatal("deleted version remained visible", err)
	}
	if err := objectstorage.VerifyVersionAbsent(t.Context(), store, first.Key, first.VersionID); err != nil {
		t.Fatal("local absence proof lost deleted version", err)
	}
	if err := objectstorage.VerifyVersionAbsent(t.Context(), store, second.Key, second.VersionID); !errors.Is(err, objectstorage.ErrConflict) {
		t.Fatal("local proof accepted retained sibling", err)
	}
	if err := objectstorage.VerifyVersionAbsent(t.Context(), New(), first.Key, first.VersionID); !errors.Is(err, objectstorage.ErrUnavailable) {
		t.Fatal("unqualified store manufactured absence", err)
	}
	if err := objectstorage.VerifyVersionAbsent(t.Context(), store, first.Key, ""); !errors.Is(err, objectstorage.ErrInvalid) {
		t.Fatal("absence proof selected latest", err)
	}
	if receipt, err := store.Head(t.Context(), second.Key, second.VersionID); err != nil || receipt != second {
		t.Fatal("exact deletion removed a sibling version", err)
	}
}

package draftruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	kube "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

type runtimeFake struct {
	create, read, lookup, deleted int
	effect                        kube.MaterializationEffect
	material                      kube.Materialization
	failure                       error
	mutate                        func(*kube.Materialization)
}

func (f *runtimeFake) Namespace() string { return "kodex-runtime" }
func (f *runtimeFake) CreateImmutableForEffect(_ context.Context, e kube.MaterializationEffect, _ []byte) (kube.Materialization, error) {
	f.create++
	f.effect = e
	return f.material, f.failure
}
func (f *runtimeFake) ReadbackExact(_ context.Context, m kube.Materialization) (kube.Materialization, error) {
	f.read++
	if f.mutate != nil {
		f.mutate(&m)
	}
	return m, f.failure
}
func (f *runtimeFake) LookupExpectedEffect(_ context.Context, e kube.MaterializationEffect) (kube.Materialization, error) {
	f.lookup++
	f.effect = e
	return f.material, f.failure
}
func (f *runtimeFake) DeleteExact(_ context.Context, m kube.Materialization) error {
	f.deleted++
	f.material = m
	return f.failure
}
func runtimeFixture(t *testing.T) (value.DraftWork, *runtimeFake, []byte) {
	t.Helper()
	plaintext := []byte("synthetic-draft-value")
	sum := sha256.Sum256(plaintext)
	digest := hex.EncodeToString(sum[:])
	w := value.DraftWork{Kind: value.DraftPublish, OperationRef: "sop_fixture", ClaimantID: "pod_fixture", ClaimGeneration: 3, RuntimeNamespace: "kodex-runtime", TargetRevision: 4, Draft: value.SecretDraft{Ref: "draft_fixture", ProjectRef: "prj_fixture", SecretRef: "sec_fixture01", Generation: 2, ValueType: "STRING"}, Binding: value.SecretDraftBinding{ProjectRef: "prj_fixture", SecretRef: "sec_fixture01", DraftRef: "draft_fixture", DraftGeneration: 2, ValueType: "STRING", ContentSHA256: digest}}
	name, err := runtimesecret.VersionedKubernetesName(w.Draft.SecretRef, w.TargetRevision)
	if err != nil {
		t.Fatal(err)
	}
	f := &runtimeFake{material: kube.Materialization{Namespace: w.RuntimeNamespace, Name: name, OperationRef: w.OperationRef, ClaimGeneration: w.ClaimGeneration, SecretRef: w.Draft.SecretRef, Key: "value", Revision: w.TargetRevision, UID: "fixture-uid", ResourceVersion: "123", ContentSHA256: digest}}
	return w, f, plaintext
}
func TestRuntimePublishReadbackLookupAndDelete(t *testing.T) {
	w, f, plaintext := runtimeFixture(t)
	store, err := New(f)
	if err != nil {
		t.Fatal(err)
	}
	material, err := store.Publish(context.Background(), w, plaintext)
	if err != nil || f.create != 1 || f.read != 1 || material.DataKey != "value" || f.effect.ClaimGeneration != 3 || f.effect.ContentSHA256 != w.Binding.ContentSHA256 {
		t.Fatal("publication did not preserve fenced effect")
	}
	found, err := store.Lookup(context.Background(), w)
	if err != nil || found != material {
		t.Fatal("lookup changed descriptor")
	}
	if err := store.Delete(context.Background(), w, material); err != nil || f.deleted != 1 || f.material.UID != material.UID || f.material.ResourceVersion != material.ResourceVersion {
		t.Fatal("delete lost exact preconditions")
	}
}
func TestRuntimeRejectsInputBeforeMaterialization(t *testing.T) {
	for name, mutate := range map[string]func(*value.DraftWork){"kind": func(w *value.DraftWork) { w.Kind = value.DraftSave }, "namespace": func(w *value.DraftWork) { w.RuntimeNamespace = "foreign" }, "claim": func(w *value.DraftWork) { w.ClaimGeneration = 0 }, "claimant": func(w *value.DraftWork) { w.ClaimantID = "" }, "binding": func(w *value.DraftWork) { w.Binding.SecretRef = "sec_foreign" }, "revision": func(w *value.DraftWork) { w.TargetRevision = 0 }} {
		t.Run(name, func(t *testing.T) {
			w, f, plaintext := runtimeFixture(t)
			mutate(&w)
			store, _ := New(f)
			if _, err := store.Publish(context.Background(), w, plaintext); !errors.Is(err, secretdrafts.ErrInvalid) || f.create != 0 {
				t.Fatal("invalid work created materialization")
			}
		})
	}
	w, f, _ := runtimeFixture(t)
	store, _ := New(f)
	if _, err := store.Publish(context.Background(), w, []byte("wrong")); !errors.Is(err, secretdrafts.ErrInvalid) || f.create != 0 {
		t.Fatal("wrong content digest accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Lookup(ctx, w); !errors.Is(err, context.Canceled) || f.lookup != 0 {
		t.Fatal("cancellation ignored")
	}
}
func TestRuntimeRejectsCorruptReadbackAndDelete(t *testing.T) {
	for name, mutate := range map[string]func(*kube.Materialization){"uid": func(v *kube.Materialization) { v.UID = "" }, "namespace": func(v *kube.Materialization) { v.Namespace = "foreign" }, "key": func(v *kube.Materialization) { v.Key = "ciphertext" }, "generation": func(v *kube.Materialization) { v.ClaimGeneration++ }, "digest": func(v *kube.Materialization) { v.ContentSHA256 = "wrong" }, "operation": func(v *kube.Materialization) { v.OperationRef = "foreign" }, "resourceVersion": func(v *kube.Materialization) { v.ResourceVersion = "456" }} {
		t.Run(name, func(t *testing.T) {
			w, f, plaintext := runtimeFixture(t)
			f.mutate = mutate
			store, _ := New(f)
			if _, err := store.Publish(context.Background(), w, plaintext); !errors.Is(err, secretdrafts.ErrConflict) {
				t.Fatal("corrupt readback accepted")
			}
		})
	}
	w, f, _ := runtimeFixture(t)
	store, _ := New(f)
	d := cast(f.material)
	d.DataKey = "ciphertext"
	if err := store.Delete(context.Background(), w, d); !errors.Is(err, secretdrafts.ErrConflict) || f.deleted != 0 {
		t.Fatal("foreign materialization deleted")
	}
}
func TestRuntimeErrorClassification(t *testing.T) {
	for failure, want := range map[error]error{kube.ErrMaterializationNotFound: secretdrafts.ErrNotFound, kube.ErrMaterializationConflict: secretdrafts.ErrConflict, kube.ErrMaterializationInvalid: secretdrafts.ErrInvalid, errors.New("private upstream detail"): secretdrafts.ErrUnavailable} {
		w, f, _ := runtimeFixture(t)
		f.failure = failure
		store, _ := New(f)
		if _, err := store.Lookup(context.Background(), w); !errors.Is(err, want) {
			t.Fatalf("wrong error: %v", err)
		}
	}
}

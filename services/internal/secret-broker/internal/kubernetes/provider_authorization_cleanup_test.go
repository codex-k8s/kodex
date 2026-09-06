package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func authorizationCleanupFixture() (ProviderAuthorizationAttempt, ProviderAuthorizationCleanupTarget) {
	const attemptRef = "pauth_cleanup123456"
	digest := sha256.Sum256([]byte(attemptRef + "\x00" + providerCleanupAccountRef))
	attempt := ProviderAuthorizationAttempt{AttemptRef: attemptRef, AccountRef: providerCleanupAccountRef,
		MaterializerAttemptRef: "pmat_" + hex.EncodeToString(digest[:16]), State: "PENDING",
		VerificationURI: "https://example.invalid/device", UserCode: "SYNTHETIC-ONLY", ExpiresAt: time.Now().Add(-time.Minute)}
	return attempt, ProviderAuthorizationCleanupTarget{TaskRef: providerCleanupTaskRef, AccountRef: attempt.AccountRef,
		AuthorizationAttemptRef: attempt.AttemptRef, MaterializerAttemptRef: attempt.MaterializerAttemptRef, Generation: providerCleanupGeneration}
}

// Fake API сохраняет настоящие UUID и применяет UID/RV CAS при Update;
// обычный object tracker сам этих Kubernetes preconditions не проверяет.
func newAuthorizationCleanupStore(t *testing.T) (*Store, *fake.Clientset) {
	t.Helper()
	store, client := newTestStore(t)
	var revision int64 = 100
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		wanted := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret).DeepCopy()
		revision++
		wanted.UID = types.UID(fmt.Sprintf("61000000-0000-4000-8000-%012d", revision))
		wanted.ResourceVersion = strconv.FormatInt(revision, 10)
		err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), wanted, wanted.Namespace)
		return true, wanted, err
	})
	client.PrependReactor("update", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		wanted := action.(k8stesting.UpdateAction).GetObject().(*corev1.Secret).DeepCopy()
		stored, err := client.Tracker().Get(corev1.SchemeGroupVersion.WithResource("secrets"), wanted.Namespace, wanted.Name)
		if err != nil {
			return true, nil, err
		}
		current := stored.(*corev1.Secret)
		if current.UID != wanted.UID || current.ResourceVersion != wanted.ResourceVersion ||
			(current.Immutable != nil && *current.Immutable && !reflect.DeepEqual(current.Data, wanted.Data)) {
			return true, nil, apierrors.NewConflict(corev1.Resource("secrets"), wanted.Name, errors.New("synthetic CAS conflict"))
		}
		revision++
		wanted.ResourceVersion = strconv.FormatInt(revision, 10)
		err = client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("secrets"), wanted, wanted.Namespace)
		return true, wanted, err
	})
	return store, client
}

func TestAuthorizationCleanupMetadataDoesNotPollOrExpire(t *testing.T) {
	t.Parallel()
	store, client := newAuthorizationCleanupStore(t)
	attempt, target := authorizationCleanupFixture()
	stored, _, err := store.CreateProviderAuthorizationAttempt(t.Context(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	observed, err := store.ObserveProviderAuthorizationCleanup(t.Context(), target)
	if err != nil || observed.State != ProviderAuthorizationPresent || observed.Target.UID != stored.SecretUID ||
		observed.Target.ResourceVersion != stored.ResourceVersion || observed.ProducedCredential != nil {
		t.Fatalf("exact metadata observation failed: %v", err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatalf("metadata observation mutated expired attempt: %s", action.GetVerb())
		}
	}
	for _, change := range []func(*ProviderAuthorizationCleanupTarget){
		func(v *ProviderAuthorizationCleanupTarget) { v.AccountRef = "pacc_different1234" },
		func(v *ProviderAuthorizationCleanupTarget) { v.MaterializerAttemptRef = "pmat_wrong1234" },
		func(v *ProviderAuthorizationCleanupTarget) { v.UID = stored.SecretUID },
		func(v *ProviderAuthorizationCleanupTarget) { v.Kind = ProviderCleanupAbsence },
		func(v *ProviderAuthorizationCleanupTarget) { v.Generation = 0 },
	} {
		wrong := target
		change(&wrong)
		client.ClearActions()
		if _, err := store.ObserveProviderAuthorizationCleanup(t.Context(), wrong); !errors.Is(err, ErrProviderCredentialCleanupInvalid) || len(client.Actions()) != 0 {
			t.Fatal("invalid metadata authority coordinates reached Kubernetes")
		}
	}
}

func TestAuthorizationCleanupFencesBothNamesAcrossRestart(t *testing.T) {
	t.Parallel()
	store, client := newAuthorizationCleanupStore(t)
	attempt, target := authorizationCleanupFixture()
	stored, _, err := store.CreateProviderAuthorizationAttempt(t.Context(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	target.Kind, target.UID, target.ResourceVersion = ProviderCleanupAuthorization, stored.SecretUID, stored.ResourceVersion
	if _, err := store.FenceAuthorizationCredentialName(t.Context(), target); !errors.Is(err, ErrProviderCredentialCleanupConflict) {
		t.Fatalf("credential fenced without pending fence: %v", err)
	}
	if err := store.FenceProviderAuthorization(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	// Crash между двумя fences остаётся незавершённой absence task.
	restarted, _ := New(client, testNamespace)
	_, query := authorizationCleanupFixture()
	observed, err := restarted.ObserveProviderAuthorizationCleanup(t.Context(), query)
	if err != nil || observed.State != ProviderAuthorizationAbsent {
		t.Fatalf("partial fence was treated as terminal or unrecoverable: %v", err)
	}
	if err := restarted.FenceProviderAuthorization(t.Context(), observed.Target); err != nil {
		t.Fatal(err)
	}
	if produced, err := restarted.FenceAuthorizationCredentialName(t.Context(), observed.Target); err != nil || produced != nil {
		t.Fatalf("credential name fencing failed: %v", err)
	}
	if _, _, err := store.CreateProviderAuthorizationAttempt(t.Context(), attempt); err == nil {
		t.Fatal("late pending creation bypassed immutable fence")
	}
	stored.State, stored.SafeFailureCode = "FAILED", "SYNTHETIC_FAILURE"
	if _, err := store.CompleteProviderAuthorizationAttempt(t.Context(), stored); err == nil {
		t.Fatal("late terminal update bypassed immutable fence")
	}
	if _, err := store.CreateProviderCredential(t.Context(), attempt.AttemptRef, attempt.AccountRef, []byte(providerCleanupContent)); err == nil {
		t.Fatal("late credential creation bypassed name fence")
	}
	observed, err = restarted.ObserveProviderAuthorizationCleanup(t.Context(), query)
	if err != nil || observed.State != ProviderAuthorizationFenced || observed.ProducedCredential != nil {
		t.Fatalf("complete durable fence was not observed: %v", err)
	}
	name, _ := providerAttemptName(target.MaterializerAttemptRef)
	fence, err := client.CoreV1().Secrets(testNamespace).Get(t.Context(), name, metav1.GetOptions{})
	if err != nil || string(fence.UID) != target.UID || fence.ResourceVersion == target.ResourceVersion || len(fence.Data) != 1 || len(fence.Data["user-code"]) != 0 {
		t.Fatal("pending fence did not atomically erase material and retain UID")
	}
}

func TestAuthorizationAbsenceCannotDeleteLatePendingObject(t *testing.T) {
	t.Parallel()
	store, client := newAuthorizationCleanupStore(t)
	attempt, query := authorizationCleanupFixture()
	observed, err := store.ObserveProviderAuthorizationCleanup(t.Context(), query)
	if err != nil || observed.State != ProviderAuthorizationAbsent {
		t.Fatal("initial absent metadata failed")
	}
	if _, _, err := store.CreateProviderAuthorizationAttempt(t.Context(), attempt); err != nil {
		t.Fatal(err)
	}
	if err := store.FenceProviderAuthorization(t.Context(), observed.Target); !errors.Is(err, ErrProviderCredentialCleanupConflict) {
		t.Fatalf("absence command changed an unpinned pending object: %v", err)
	}
	observed, err = store.ObserveProviderAuthorizationCleanup(t.Context(), query)
	if err != nil || observed.State != ProviderAuthorizationPresent {
		t.Fatal("owner could not recover exact late pending metadata")
	}
	wrong := observed.Target
	wrong.ResourceVersion = "stale-version"
	if err := store.FenceProviderAuthorization(t.Context(), wrong); !errors.Is(err, ErrProviderCredentialCleanupConflict) {
		t.Fatal("stale exact pending target was accepted")
	}
	name, _ := providerAttemptName(query.MaterializerAttemptRef)
	if providerSecretActionCount(client, "delete", name) != 0 || providerSecretActionCount(client, "update", name) != 0 {
		t.Fatal("unfenced/stale target mutated pending object")
	}
}

func TestAuthorizationProducedCredentialReceiptSurvivesCleanupAndLostACK(t *testing.T) {
	t.Parallel()
	store, client := newAuthorizationCleanupStore(t)
	attempt, target := authorizationCleanupFixture()
	target.Kind = ProviderCleanupAbsence
	produced, err := store.CreateProviderCredential(t.Context(), attempt.AttemptRef, attempt.AccountRef, []byte(providerCleanupContent))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FenceProviderAuthorization(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	actual, err := store.FenceAuthorizationCredentialName(t.Context(), target)
	if err != nil || actual == nil || *actual != produced {
		t.Fatalf("produced credential was lost: %v", err)
	}
	identity, _ := AuthorizationCleanupReceiptIdentity(target)
	loseACK := true
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		secret := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret).DeepCopy()
		if secret.Name != providerCleanupReceiptName(identity) || !loseACK {
			return false, nil, nil
		}
		loseACK = false
		secret.UID, secret.ResourceVersion = "61000000-0000-4000-8000-000000000999", "999"
		if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), secret, testNamespace); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewServiceUnavailable("synthetic lost ACK")
	})
	if _, err := store.CompleteProviderCleanupReceipt(t.Context(), identity, actual); err == nil {
		t.Fatal("lost receipt ACK was reported as success")
	}
	restarted, _ := New(client, testNamespace)
	replayed, found, err := restarted.ReadProviderCleanupReceipt(t.Context(), identity)
	if err != nil || !found || replayed.ProducedCredential == nil || *replayed.ProducedCredential != produced {
		t.Fatalf("lost ACK recovery changed produced descriptor: %v", err)
	}
	terminal, err := restarted.CleanupProviderCredential(t.Context(), "pcct_produced1234", target.AccountRef, 1, produced)
	if err != nil || terminal.ProducedCredential != nil || terminal.TerminalReceipt == "" {
		t.Fatalf("separate produced cleanup failed: %v", err)
	}
	after, found, err := store.ReadProviderCleanupReceipt(t.Context(), identity)
	if err != nil || !found || !reflect.DeepEqual(replayed, after) {
		t.Fatal("old cleanup receipt changed after produced credential deletion")
	}
	changed := identity
	changed.Generation++
	if _, found, err := store.ReadProviderCleanupReceipt(t.Context(), changed); err != nil || found {
		t.Fatal("cleanup receipt crossed generation boundary")
	}
}

func TestCredentialCleanupReportsLateCreateRaceAsSeparateTarget(t *testing.T) {
	t.Parallel()
	store, client := newAuthorizationCleanupStore(t)
	attempt, target := authorizationCleanupFixture()
	original, err := store.CreateProviderCredential(t.Context(), attempt.AttemptRef, attempt.AccountRef, []byte(providerCleanupContent))
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := client.CoreV1().Secrets(testNamespace).Get(t.Context(), original.SecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	replacement.UID, replacement.ResourceVersion = "61000000-0000-4000-8000-000000000777", "777"
	injected := false
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		secret := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret)
		if secret.Name != original.SecretName || injected {
			return false, nil, nil
		}
		injected = true
		if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), replacement, testNamespace); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewAlreadyExists(corev1.Resource("secrets"), secret.Name)
	})
	first, err := store.CleanupProviderCredential(t.Context(), target.TaskRef, target.AccountRef, target.Generation, original)
	if err != nil || first.ProducedCredential == nil || first.ProducedCredential.SecretUID != string(replacement.UID) {
		t.Fatalf("late create race lost replacement descriptor: %v", err)
	}
	if providerSecretActionCount(client, "delete", original.SecretName) != 1 {
		t.Fatal("cleanup silently deleted replacement")
	}
	second, err := store.CleanupProviderCredential(t.Context(), "pcct_replacement1234", target.AccountRef, 1, *first.ProducedCredential)
	if err != nil || second.ProducedCredential != nil {
		t.Fatalf("replacement cleanup failed: %v", err)
	}
	replay, err := store.CleanupProviderCredential(t.Context(), target.TaskRef, target.AccountRef, target.Generation, original)
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatal("credential cleanup retry lost its prior produced descriptor")
	}
}

func TestAuthorizationFenceUnknownUpdateACKRecoversExactOriginalPins(t *testing.T) {
	t.Parallel()
	store, client := newAuthorizationCleanupStore(t)
	attempt, target := authorizationCleanupFixture()
	stored, _, err := store.CreateProviderAuthorizationAttempt(context.Background(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	target.Kind, target.UID, target.ResourceVersion = ProviderCleanupAuthorization, stored.SecretUID, stored.ResourceVersion
	client.PrependReactor("update", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		secret := action.(k8stesting.UpdateAction).GetObject().(*corev1.Secret).DeepCopy()
		secret.ResourceVersion = "998"
		if err := client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("secrets"), secret, testNamespace); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewServiceUnavailable("synthetic lost ACK")
	})
	if err := store.FenceProviderAuthorization(t.Context(), target); err == nil {
		t.Fatal("unknown fence update ACK was reported as success")
	}
	restarted, _ := New(client, testNamespace)
	if err := restarted.FenceProviderAuthorization(t.Context(), target); err != nil {
		t.Fatalf("durable original pins did not recover unknown ACK: %v", err)
	}
	target.UID = "61000000-0000-4000-8000-000000000666"
	if err := restarted.FenceProviderAuthorization(t.Context(), target); !errors.Is(err, ErrProviderCredentialCleanupConflict) {
		t.Fatal("fence replay accepted different original UID")
	}
}

func TestAuthorizationFenceRejectsConcurrentResourceVersionChange(t *testing.T) {
	t.Parallel()
	store, client := newAuthorizationCleanupStore(t)
	attempt, target := authorizationCleanupFixture()
	stored, _, err := store.CreateProviderAuthorizationAttempt(t.Context(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	target.Kind, target.UID, target.ResourceVersion = ProviderCleanupAuthorization, stored.SecretUID, stored.ResourceVersion
	client.PrependReactor("update", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		wanted := action.(k8stesting.UpdateAction).GetObject().(*corev1.Secret)
		object, err := client.Tracker().Get(corev1.SchemeGroupVersion.WithResource("secrets"), testNamespace, wanted.Name)
		if err != nil {
			t.Fatal(err)
		}
		current := object.(*corev1.Secret)
		current.ResourceVersion = "999"
		if err := client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("secrets"), current, testNamespace); err != nil {
			t.Fatal(err)
		}
		return false, nil, nil
	})
	if err := store.FenceProviderAuthorization(t.Context(), target); !errors.Is(err, ErrProviderCredentialCleanupConflict) {
		t.Fatalf("concurrent pending mutation bypassed CAS: %v", err)
	}
	readback, err := store.GetProviderAuthorizationAttempt(t.Context(), target.MaterializerAttemptRef)
	if err != nil || readback.State != "PENDING" || readback.ResourceVersion != "999" {
		t.Fatal("cleanup overwrote a newer pending object")
	}
}

func TestAuthorizationCleanupRejectsMalformedDurableFence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*corev1.Secret)
	}{
		{"mutable", func(s *corev1.Secret) { s.Immutable = nil }},
		{"namespace", func(s *corev1.Secret) { s.Namespace = "other-namespace" }},
		{"manager", func(s *corev1.Secret) { s.Labels[providerManagedByLabel] = "other-manager" }},
		{"extra material", func(s *corev1.Secret) { s.Data["user-code"] = []byte("synthetic-only") }},
		{"missing UID", func(s *corev1.Secret) { s.UID = "" }},
		{"trailing JSON", func(s *corev1.Secret) {
			s.Data[providerCleanupFenceKey] = append(s.Data[providerCleanupFenceKey], []byte("{}")...)
		}},
		{"duplicate field", func(s *corev1.Secret) {
			s.Data[providerCleanupFenceKey] = append([]byte(`{"Kind":"AUTHORIZATION",`), s.Data[providerCleanupFenceKey][1:]...)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store, client := newAuthorizationCleanupStore(t)
			_, target := authorizationCleanupFixture()
			target.Kind = ProviderCleanupAbsence
			if err := store.FenceProviderAuthorization(t.Context(), target); err != nil {
				t.Fatal(err)
			}
			name, _ := providerAttemptName(target.MaterializerAttemptRef)
			secret, err := client.CoreV1().Secrets(testNamespace).Get(t.Context(), name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			test.change(secret)
			client.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
				if action.(k8stesting.GetAction).GetName() == name {
					return true, secret.DeepCopy(), nil
				}
				return false, nil, nil
			})
			client.ClearActions()
			if err := store.FenceProviderAuthorization(t.Context(), target); !errors.Is(err, ErrProviderCredentialCleanupConflict) {
				t.Fatalf("malformed durable fence was accepted: %v", err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("malformed fence triggered a materialization or mutation")
				}
			}
		})
	}
}

func TestProviderDiscardKeepsAPIFailuresRetryable(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"pending", "credential"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			store, client := newAuthorizationCleanupStore(t)
			attempt, _ := authorizationCleanupFixture()
			stored, _, err := store.CreateProviderAuthorizationAttempt(t.Context(), attempt)
			if err != nil {
				t.Fatal(err)
			}
			descriptor, err := store.CreateProviderCredential(t.Context(), attempt.AttemptRef, attempt.AccountRef, []byte(providerCleanupContent))
			if err != nil {
				t.Fatal(err)
			}
			client.ClearActions()
			client.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewServiceUnavailable("synthetic API outage")
			})
			if kind == "pending" {
				err = store.DiscardProviderAuthorizationAttempt(t.Context(), attempt.AttemptRef, attempt.AccountRef, attempt.MaterializerAttemptRef, stored.SecretUID, stored.ResourceVersion)
			} else {
				err = store.DiscardProviderCredential(t.Context(), attempt.AttemptRef, attempt.AccountRef, descriptor)
			}
			if err == nil || errors.Is(err, ErrProviderAttemptConflict) || errors.Is(err, ErrProviderCredentialConflict) || errors.Is(err, ErrProviderCredentialCleanupConflict) {
				t.Fatalf("transient API failure was converted into terminal conflict: %v", err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("discard API failure triggered an effect")
				}
			}
		})
	}
}

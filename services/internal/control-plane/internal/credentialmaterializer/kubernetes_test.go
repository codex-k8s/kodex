package credentialmaterializer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestMaterializeCreatesAnExactSecretKeyAndSupportsSafeRetry(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kodex-system", Name: "kodex-integration-credentials",
			UID: types.UID("30000000-0000-4000-8000-000000000001"), ResourceVersion: "7",
		},
		Data: map[string][]byte{"existing": []byte("preserved")},
	})
	materializer, err := New(client, "kodex-system", "kodex-integration-credentials")
	if err != nil {
		t.Fatalf("construct materializer: %v", err)
	}
	credential := []byte("integration-token")
	wantDigest := sha256.Sum256(credential)
	created, err := materializer.Materialize(context.Background(), "integration-001", credential)
	if err != nil {
		t.Fatalf("materialize credential: %v", err)
	}
	if created.SecretRef != "kodex-system/kodex-integration-credentials#integration-001" ||
		created.SecretUID != "30000000-0000-4000-8000-000000000001" ||
		created.SecretResourceVersion == "" || created.ContentSHA256 != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("unexpected safe metadata: %#v", created)
	}
	stored, err := client.CoreV1().Secrets("kodex-system").Get(context.Background(), "kodex-integration-credentials", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read materialized secret: %v", err)
	}
	if string(stored.Data["existing"]) != "preserved" || string(stored.Data["integration-001"]) != "integration-token" {
		t.Fatal("materializer replaced unrelated secret data or stored wrong credential")
	}
	retried, err := materializer.Materialize(context.Background(), "integration-001", credential)
	if err != nil || retried.SecretRef != created.SecretRef || retried.ContentSHA256 != created.ContentSHA256 {
		t.Fatalf("safe retry failed: metadata=%#v err=%v", retried, err)
	}
}

func TestMaterializeRejectsIdempotencyKeyReuseWithDifferentCredential(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kodex-system", Name: "kodex-integration-credentials",
			UID: types.UID("30000000-0000-4000-8000-000000000001"), ResourceVersion: "7",
		},
		Data: map[string][]byte{"integration-001": []byte("first-token")},
	})
	materializer, err := New(client, "kodex-system", "kodex-integration-credentials")
	if err != nil {
		t.Fatalf("construct materializer: %v", err)
	}
	_, err = materializer.Materialize(context.Background(), "integration-001", []byte("different-token"))
	if !errors.Is(err, platformservice.ErrCredentialMaterializationConflict) {
		t.Fatalf("credential reuse returned %v, want materialization conflict", err)
	}
}

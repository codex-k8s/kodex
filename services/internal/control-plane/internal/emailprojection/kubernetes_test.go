package emailprojection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func projectionFixture(t *testing.T) (*Kubernetes, *fake.Clientset, api.Configuration) {
	t.Helper()
	raw, err := os.ReadFile("../../../../../contracts/email-bridge/v1/examples/mailboxes.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var configuration api.Configuration
	if api.Decode(raw, &configuration) != nil {
		t.Fatal("decode fixture")
	}
	configuration.Revision = 2
	seed := api.Configuration{Version: "email-bridge/v1", Revision: 1, ManagedBy: "git", Source: "release-bootstrap", Mailboxes: []api.Mailbox{}}
	encoded, _ := json.Marshal(seed)
	data := map[string][]byte{DocumentKey: encoded}
	for _, mailbox := range configuration.Mailboxes {
		for _, endpoint := range []*api.Endpoint{&mailbox.Smtp, mailbox.Imap, mailbox.Pop} {
			if endpoint == nil {
				continue
			}
			for _, descriptor := range []api.Descriptor{endpoint.Ca, endpoint.Username, endpoint.Secret} {
				key, err := CredentialKey(descriptor)
				if err != nil {
					t.Fatal(err)
				}
				data[key] = []byte("synthetic-fixture-material")
			}
		}
	}
	client := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: "kodex-system", Name: SecretName, UID: "fixture-secret", ResourceVersion: "1",
	}, Data: data})
	publisher, err := New(client, "kodex-system")
	if err != nil {
		t.Fatal(err)
	}
	return publisher, client, configuration
}

func TestPublishExactSnapshotAndNoCredentialMutation(t *testing.T) {
	publisher, client, configuration := projectionFixture(t)
	ctx := context.Background()
	before, _ := client.CoreV1().Secrets("kodex-system").Get(ctx, SecretName, metav1.GetOptions{})
	receipt, err := publisher.Publish(ctx, configuration, fixtureCredentialDigests(configuration))
	if err != nil || receipt.Revision != 2 || receipt.Digest != api.Digest(configuration) || receipt.SecretUID != "fixture-secret" || receipt.ResourceVersion == "" {
		t.Fatalf("publish/readback: %#v %v", receipt, err)
	}
	after, _ := client.CoreV1().Secrets("kodex-system").Get(ctx, SecretName, metav1.GetOptions{})
	for key, value := range before.Data {
		if key != DocumentKey && string(after.Data[key]) != string(value) {
			t.Fatal("credential mutated")
		}
	}
	client.ClearActions()
	if _, err := publisher.Publish(ctx, configuration, fixtureCredentialDigests(configuration)); err != nil {
		t.Fatal(err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" || action.GetResource().Resource != "secrets" || action.(k8stesting.GetAction).GetName() != SecretName {
			t.Fatal("exact replay must only read the named secret")
		}
	}
	configuration.Source = "different-source"
	if _, err := publisher.Publish(ctx, configuration, fixtureCredentialDigests(configuration)); !errors.Is(err, ErrConflict) {
		t.Fatalf("equivocation: %v", err)
	}
	configuration.Revision = 1
	if _, err := publisher.Publish(ctx, configuration, fixtureCredentialDigests(configuration)); !errors.Is(err, ErrConflict) {
		t.Fatalf("rollback: %v", err)
	}
}

func TestPublishRequiresExactCredentialsAndExistingSeed(t *testing.T) {
	for _, mode := range []string{"missing", "generation", "empty", "changed-value", "absent-secret", "corrupt-document"} {
		t.Run(mode, func(t *testing.T) {
			publisher, client, configuration := projectionFixture(t)
			ctx := context.Background()
			secret, _ := client.CoreV1().Secrets("kodex-system").Get(ctx, SecretName, metav1.GetOptions{})
			key, _ := CredentialKey(configuration.Mailboxes[0].Smtp.Secret)
			switch mode {
			case "changed-value":
				secret.Data[key] = []byte("unexpected-replacement")
			case "missing":
				delete(secret.Data, key)
			case "empty":
				secret.Data[key] = nil
			case "generation":
				configuration.Mailboxes[0].Smtp.Secret.Generation++
			case "corrupt-document":
				secret.Data[DocumentKey] = []byte("invalid")
			}
			if mode == "absent-secret" {
				if err := client.CoreV1().Secrets("kodex-system").Delete(ctx, SecretName, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			} else if _, err := client.CoreV1().Secrets("kodex-system").Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			client.ClearActions()
			if _, err := publisher.Publish(ctx, configuration, fixtureCredentialDigests(configuration)); err == nil {
				t.Fatal("unsafe projection accepted")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("invalid projection mutated state")
				}
			}
		})
	}
}

func TestPublishReadbackRejectsLostUpdate(t *testing.T) {
	publisher, client, configuration := projectionFixture(t)
	client.PrependReactor("update", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, action.(k8stesting.UpdateAction).GetObject(), nil
	})
	if _, err := publisher.Publish(context.Background(), configuration, fixtureCredentialDigests(configuration)); !errors.Is(err, ErrConflict) {
		t.Fatalf("unserved update accepted: %v", err)
	}
}

func TestEmptyProjectionNeedsNoFakeCredential(t *testing.T) {
	publisher, client, configuration := projectionFixture(t)
	configuration.Mailboxes = []api.Mailbox{}
	ctx := context.Background()
	secret, _ := client.CoreV1().Secrets("kodex-system").Get(ctx, SecretName, metav1.GetOptions{})
	secret.Data = map[string][]byte{DocumentKey: secret.Data[DocumentKey]}
	if _, err := client.CoreV1().Secrets("kodex-system").Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(ctx, configuration, fixtureCredentialDigests(configuration)); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialKeyRejectsPathsAndSeparatesGenerations(t *testing.T) {
	for _, name := range []string{"", "../secret", "ca.1", "ca/1"} {
		if _, err := CredentialKey(api.Descriptor{Name: name, Generation: 1}); err == nil {
			t.Fatal("invalid descriptor accepted")
		}
	}
	if _, err := CredentialKey(api.Descriptor{Name: "ca", Generation: 0}); err == nil {
		t.Fatal("zero generation")
	}
	key, err := CredentialKey(api.Descriptor{Name: "ca", Generation: 12})
	if err != nil || key != "ca.12" {
		t.Fatal("descriptor key mismatch")
	}
}

func fixtureCredentialDigests(configuration api.Configuration) map[string]string {
	digest := sha256.Sum256([]byte("synthetic-fixture-material"))
	result := map[string]string{}
	for _, mailbox := range configuration.Mailboxes {
		for _, endpoint := range []*api.Endpoint{&mailbox.Smtp, mailbox.Imap, mailbox.Pop} {
			if endpoint == nil {
				continue
			}
			for _, descriptor := range []api.Descriptor{endpoint.Ca, endpoint.Username, endpoint.Secret} {
				key, _ := CredentialKey(descriptor)
				result[key] = hex.EncodeToString(digest[:])
			}
		}
	}
	return result
}

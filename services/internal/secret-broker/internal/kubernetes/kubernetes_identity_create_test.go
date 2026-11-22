package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// kubernetesIdentityProvisionFixture derives synthetic pre-create pins without a placeholder UID/RV.
// kubernetesIdentityProvisionFixture задаёт synthetic pre-create pins без фиктивного UID/RV.
func kubernetesIdentityProvisionFixture(t *testing.T) (*corev1.Secret, value.KubernetesIdentityProvision) {
	t.Helper()
	secret, locator := kubernetesIdentityStoreFixture(t)
	return secret, value.KubernetesIdentityProvision{IdentityRef: locator.IdentityRef, TargetRef: locator.TargetRef,
		Revision: locator.Revision, Namespace: locator.Namespace, ContentSHA256: locator.ContentSHA256}
}

// kubernetesIdentityWire owns a local TLS API fixture and synchronized request evidence.
// kubernetesIdentityWire владеет локальной TLS API оснасткой и синхронизированными счётчиками запросов.
type kubernetesIdentityWire struct {
	mu          sync.Mutex
	posts, gets int
	status      int
	dropACK     bool
	ackMode     string
	stored      *corev1.Secret
	expected    *corev1.Secret
}

// serveHTTP checks the transport and exact purpose route without printing private request bodies.
// serveHTTP проверяет транспорт и точный purpose route без вывода приватного тела запроса.
func (wire *kubernetesIdentityWire) serveHTTP(w http.ResponseWriter, r *http.Request) {
	wire.mu.Lock()
	defer wire.mu.Unlock()
	if r.TLS == nil || r.Header.Get("Authorization") != "Bearer synthetic-storage-auth" {
		http.Error(w, "invalid synthetic transport", http.StatusForbidden)
		return
	}
	path := "/api/v1/namespaces/" + wire.expected.Namespace + "/secrets"
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		wire.gets++
	}
	if r.Method == http.MethodPost {
		wire.posts++
	}
	if r.Method == http.MethodGet && r.URL.Path == path+"/"+wire.expected.Name {
		if wire.stored == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Failure","reason":"NotFound","code":404}`))
			return
		}
		_ = json.NewEncoder(w).Encode(wire.stored)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != path {
		http.Error(w, "invalid synthetic route", http.StatusBadRequest)
		return
	}
	created := new(corev1.Secret)
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, maximumKubernetesIdentityBytes*2)).Decode(created) != nil ||
		created.UID != "" || created.ResourceVersion != "" || created.Name != wire.expected.Name ||
		created.Namespace != wire.expected.Namespace || len(created.OwnerReferences) != 0 ||
		created.Labels[managedLabel] != "" || len(created.Data) != 1 || created.Immutable == nil || !*created.Immutable ||
		!bytes.Equal(created.Data[kubernetesIdentityDataKey], wire.expected.Data[kubernetesIdentityDataKey]) {
		http.Error(w, "invalid synthetic object", http.StatusBadRequest)
		return
	}
	defer clearSecretData(created)
	// The ordinary typed SDK control gets one retryable response, then a successful response.
	// Контрольный обычный typed SDK получает один retryable response, затем успешный response.
	if wire.status != 0 && (wire.status != http.StatusTooManyRequests || wire.posts == 1) {
		w.Header().Set("Retry-After", "1")
		w.Header().Set("Location", path)
		w.WriteHeader(wire.status)
		_ = json.NewEncoder(w).Encode(&metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"},
			Status: metav1.StatusFailure, Reason: metav1.StatusReasonUnknown, Code: int32(wire.status), Message: "synthetic private upstream detail"})
		return
	}
	created.UID, created.ResourceVersion = wire.expected.UID, wire.expected.ResourceVersion
	created.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}
	wire.stored = created.DeepCopy()
	if wire.dropACK {
		connection, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = connection.Close()
		}
		return
	}
	w.WriteHeader(http.StatusCreated)
	switch wire.ackMode {
	case "wrong-content":
		created.Data[kubernetesIdentityDataKey] = []byte("synthetic-invalid-successful-body")
	case "missing-uid":
		created.UID = ""
	case "invalid-json":
		_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Secret","metadata":`))
		return
	}
	_ = json.NewEncoder(w).Encode(created)
}

// TestKubernetesIdentityInvalidSuccessfulAcknowledgementIsTerminal separates complete bad ACKs
// from joined transport loss; an admitted observation must never repair this successful response.
// TestKubernetesIdentityInvalidSuccessfulAcknowledgementIsTerminal отделяет complete bad ACK
// от joined transport loss; admitted observation не должна исправлять такой successful response.
func TestKubernetesIdentityInvalidSuccessfulAcknowledgementIsTerminal(t *testing.T) {
	for _, mode := range []string{"wrong-content", "missing-uid", "invalid-json"} {
		t.Run(mode, func(t *testing.T) {
			store, wire, _, provision := newKubernetesIdentityWire(t, 0, false)
			wire.mu.Lock()
			wire.ackMode = mode
			wire.mu.Unlock()
			locator, err := store.CreateKubernetesIdentity(t.Context(), provision, wire.expected.Data[kubernetesIdentityDataKey])
			posts, gets := wire.counts()
			var rejected *targetrepo.IdentityAcknowledgementRejection
			if !errors.Is(err, targetrepo.ErrIdentityAcknowledgementRejected) || errors.Is(err, ErrKubernetesIdentityCreateUnknown) ||
				!errors.As(err, &rejected) || locator != (value.KubernetesIdentityLocator{}) || posts != 1 || gets != 0 {
				t.Fatal("completed invalid successful ACK became observable transport ambiguity")
			}
			if !rejected.Receipt.Valid() || rejected.Receipt.Truncated || err.Error() != targetrepo.ErrIdentityAcknowledgementRejected.Error() {
				t.Fatal("rejected ACK lost its bounded private receipt or leaked diagnostic details")
			}
			if mode == "wrong-content" && rejected.Receipt.ObservedUID != string(wire.expected.UID) ||
				mode != "wrong-content" && rejected.Receipt.ObservedUID != "" {
				t.Fatal("rejected ACK lost a known UID or invented an unparsed witness")
			}
		})
	}
}

// counts returns race-safe HTTP evidence after the adapter has joined its request.
// counts возвращает безопасные для race HTTP счётчики после завершения запроса адаптера.
func (wire *kubernetesIdentityWire) counts() (int, int) {
	wire.mu.Lock()
	defer wire.mu.Unlock()
	return wire.posts, wire.gets
}

// newKubernetesIdentityWire constructs a real SDK client with exact local CA and synthetic bearer.
// newKubernetesIdentityWire создаёт настоящий SDK клиент с точной локальной CA и synthetic bearer.
func newKubernetesIdentityWire(t *testing.T, status int, dropACK bool) (*Store, *kubernetesIdentityWire, *kubernetes.Clientset, value.KubernetesIdentityProvision) {
	t.Helper()
	secret, provision := kubernetesIdentityProvisionFixture(t)
	wire := &kubernetesIdentityWire{expected: secret, status: status, dropACK: dropACK}
	server := httptest.NewTLSServer(http.HandlerFunc(wire.serveHTTP))
	t.Cleanup(server.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL, BearerToken: "synthetic-storage-auth",
		TLSClientConfig: rest.TLSClientConfig{CAData: ca}, QPS: -1, Timeout: 3 * time.Second,
		ContentConfig: rest.ContentConfig{ContentType: runtime.ContentTypeJSON, AcceptContentTypes: runtime.ContentTypeJSON}})
	if err != nil {
		t.Fatal("construct synthetic TLS client")
	}
	store, err := New(client, provision.Namespace)
	if err != nil {
		t.Fatal("construct synthetic purpose writer")
	}
	return store, wire, client, provision
}

// TestCreateKubernetesIdentityUsesOnePurposeOnlyCreate checks actual TLS requests, custody and pins.
// TestCreateKubernetesIdentityUsesOnePurposeOnlyCreate проверяет реальные TLS запросы, custody и pins.
func TestCreateKubernetesIdentityUsesOnePurposeOnlyCreate(t *testing.T) {
	store, wire, _, provision := newKubernetesIdentityWire(t, 0, false)
	material := append([]byte(nil), wire.expected.Data[kubernetesIdentityDataKey]...)
	defer clear(material)
	locator, err := store.CreateKubernetesIdentity(context.Background(), provision, material)
	posts, gets := wire.counts()
	if err != nil || locator.UID != string(wire.expected.UID) || locator.ResourceVersion != wire.expected.ResourceVersion ||
		locator.Validate(provision.Namespace) != nil || !bytes.Equal(material, wire.expected.Data[kubernetesIdentityDataKey]) || posts != 1 || gets != 0 {
		t.Fatal("single purpose create lost custody or immutable server pins")
	}
	read, err := store.ReadKubernetesIdentity(context.Background(), locator)
	defer clear(read)
	if err != nil || !bytes.Equal(read, material) {
		t.Fatal("exact private read failed")
	}
	posts, gets = wire.counts()
	if posts != 1 || gets != 1 {
		t.Fatal("private read performed an unexpected HTTP effect")
	}
}

// TestCreateKubernetesIdentityNeverRetriesWireErrors proves one POST for retryable responses and lost ACK.
// TestCreateKubernetesIdentityNeverRetriesWireErrors доказывает один POST при retryable response и потере ACK.
func TestCreateKubernetesIdentityNeverRetriesWireErrors(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		dropACK bool
	}{
		{"429", http.StatusTooManyRequests, false}, {"503", http.StatusServiceUnavailable, false},
		{"already-exists", http.StatusConflict, false}, {"lost-ack", 0, true},
		{"redirect-302", http.StatusFound, false}, {"redirect-307", http.StatusTemporaryRedirect, false}, {"redirect-308", http.StatusPermanentRedirect, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, wire, _, provision := newKubernetesIdentityWire(t, test.status, test.dropACK)
			locator, err := store.CreateKubernetesIdentity(context.Background(), provision, wire.expected.Data[kubernetesIdentityDataKey])
			posts, gets := wire.counts()
			if !errors.Is(err, ErrKubernetesIdentityCreateUnknown) || locator != (value.KubernetesIdentityLocator{}) ||
				posts != 1 || gets != 0 || strings.Contains(err.Error(), "upstream detail") {
				t.Fatalf("ambiguous write retried, reconciled implicitly or leaked private details (POST=%d GET=%d)", posts, gets)
			}
			if test.dropACK {
				for range 2 {
					observed, err := store.ObserveKubernetesIdentity(context.Background(), provision)
					if err != nil || observed.UID != string(wire.expected.UID) {
						t.Fatal("explicit read-only rejoin failed")
					}
				}
				posts, gets = wire.counts()
				if posts != 1 || gets != 2 {
					t.Fatal("explicit rejoin repeated a write")
				}
			}
		})
	}
}

// TestTypedIdentityCreateControlRetriesHTTP proves the fixture exposes the pinned SDK retry behavior.
// TestTypedIdentityCreateControlRetriesHTTP доказывает, что оснастка выявляет повторы закреплённого SDK.
func TestTypedIdentityCreateControlRetriesHTTP(t *testing.T) {
	_, wire, client, provision := newKubernetesIdentityWire(t, http.StatusTooManyRequests, false)
	secret := wire.expected.DeepCopy()
	secret.UID, secret.ResourceVersion = "", ""
	defer clearSecretData(secret)
	created, err := client.CoreV1().Secrets(provision.Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	defer clearSecretData(created)
	posts, gets := wire.counts()
	if err != nil || posts != 2 || gets != 0 {
		t.Fatal("TLS control did not reproduce typed SDK retry")
	}
}

// TestIdentityCreateInvalidInputNeverReachesTLS proves local validation precedes any HTTP request.
// TestIdentityCreateInvalidInputNeverReachesTLS доказывает локальный отказ до любого HTTP запроса.
func TestIdentityCreateInvalidInputNeverReachesTLS(t *testing.T) {
	store, wire, _, provision := newKubernetesIdentityWire(t, 0, false)
	for _, material := range [][]byte{nil, []byte("different-private-bytes"), make([]byte, managedkubernetes.MaximumCredentialBytes+1)} {
		if _, err := store.CreateKubernetesIdentity(context.Background(), provision, material); err == nil {
			t.Fatal("invalid input was accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.CreateKubernetesIdentity(ctx, provision, wire.expected.Data[kubernetesIdentityDataKey]); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled input was accepted")
	}
	posts, gets := wire.counts()
	if posts != 0 || gets != 0 {
		t.Fatal("invalid input reached TLS API")
	}
	fakeStore, _ := New(fake.NewClientset(), provision.Namespace)
	if _, err := fakeStore.CreateKubernetesIdentity(context.Background(), provision, wire.expected.Data[kubernetesIdentityDataKey]); !errors.Is(err, ErrKubernetesIdentityUnavailable) {
		t.Fatal("missing concrete transport was silently replaced")
	}
}

// TestIdentityProvisionRejectsInvalidInputAndUnknownObservation prevents malformed writes and
// refuses missing, generic or foreign existing objects without repair, overwrite or new creation.
// TestIdentityProvisionRejectsInvalidInputAndUnknownObservation запрещает malformed writes
// и отвергает missing, generic либо foreign objects без repair, overwrite или нового create.
func TestIdentityProvisionRejectsInvalidInputAndUnknownObservation(t *testing.T) {
	secret, provision := kubernetesIdentityProvisionFixture(t)
	for _, material := range [][]byte{nil, []byte("different-private-bytes"), make([]byte, managedkubernetes.MaximumCredentialBytes+1)} {
		client := fake.NewClientset()
		store, _ := New(client, provision.Namespace)
		if _, err := store.CreateKubernetesIdentity(context.Background(), provision, material); err == nil || len(client.Actions()) != 0 {
			t.Fatal("invalid material reached Kubernetes create")
		}
	}
	for _, mutate := range []func(*corev1.Secret){
		func(s *corev1.Secret) { s.Labels[managedLabel] = "" },
		func(s *corev1.Secret) { s.Annotations[kubernetesIdentityTargetAnnotation] = "foreign_target" },
		func(s *corev1.Secret) { s.Data[kubernetesIdentityDataKey] = []byte("different-private-content") },
	} {
		existing := secret.DeepCopy()
		mutate(existing)
		store, wire, _, _ := newKubernetesIdentityWire(t, 0, false)
		wire.mu.Lock()
		wire.stored = existing
		wire.mu.Unlock()
		_, err := store.ObserveKubernetesIdentity(context.Background(), provision)
		posts, gets := wire.counts()
		if !errors.Is(err, targetrepo.ErrIdentityAcknowledgementRejected) || posts != 0 || gets != 1 {
			t.Fatal("foreign or mixed-purpose object became materialized")
		}
	}
	store, wire, _, _ := newKubernetesIdentityWire(t, 0, false)
	if _, err := store.ObserveKubernetesIdentity(context.Background(), provision); !errors.Is(err, ErrKubernetesIdentityUnavailable) {
		t.Fatal("missing purpose object triggered a write or became materialized")
	}
	if posts, gets := wire.counts(); posts != 0 || gets != 1 {
		t.Fatal("missing purpose observation did not perform one explicit read")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.CreateKubernetesIdentity(ctx, provision, secret.Data[kubernetesIdentityDataKey]); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled materialization reached an external effect")
	}
	if posts, gets := wire.counts(); posts != 0 || gets != 1 {
		t.Fatal("cancelled materialization changed HTTP request counts")
	}
}

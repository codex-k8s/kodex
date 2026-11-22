package kubernetes

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// sshMaterialWire owns an actual TLS API fixture with exact private paths and configurable acknowledgements; its stored object and counters are synthetic and never leave the local process.
// sshMaterialWire владеет actual TLS API fixture с exact private paths и configurable acknowledgements; synthetic object и counters не выходят из local process.
type sshMaterialWire struct {
	mutex          sync.Mutex
	secret         *corev1.Secret
	provision      value.SSHIdentityProvision
	createStatus   int
	echoBody       bool
	createDrop     bool
	createReadDrop bool
	createACK      string
	getACK         string
	getStatus      int
	getReadDrop    bool
	deleteStatus   int
	deleteRetain   bool
	lastDelete     metav1.DeleteOptions
	redirectCode   int
	redirectVerb   string
	redirectHits   atomic.Int32
	posts          atomic.Int32
	gets           atomic.Int32
	deletes        atomic.Int32
}

// newSSHMaterialWire constructs the real pinned client-go REST transport with a trusted ephemeral CA and TLS1.3, without kubeconfig, skip verification, proxy or live cluster access.
// newSSHMaterialWire создаёт настоящий pinned client-go REST transport с trusted ephemeral CA и TLS1.3 без kubeconfig, skip verification, proxy либо live cluster access.
func newSSHMaterialWire(t *testing.T) (*Store, *sshMaterialWire, []byte) {
	t.Helper()
	locator, secret := sshMaterialFixture()
	fixture := &sshMaterialWire{provision: locator.Provision()}
	server := httptest.NewUnstartedServer(http.HandlerFunc(fixture.serve))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	server.StartTLS()
	t.Cleanup(server.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL, BearerToken: "synthetic-ssh-material", Timeout: 2 * time.Second, TLSClientConfig: rest.TLSClientConfig{CAData: ca}, ContentConfig: rest.ContentConfig{ContentType: "application/json", AcceptContentTypes: "application/json"}})
	if err != nil {
		t.Fatal("create isolated purpose API transport")
	}
	store, err := New(client, locator.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	return store, fixture, secret.Data[sshIdentityMaterialKey]
}

// serve handles only the fixed namespaced purpose API and never logs request bodies, bearer values or response data; failures use synthetic status diagnostics.
// serve обслуживает только fixed namespaced purpose API без логирования request bodies, bearer values либо response data; failures используют synthetic status diagnostics.
func (fixture *sshMaterialWire) serve(writer http.ResponseWriter, request *http.Request) {
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	if request.TLS == nil || request.TLS.Version != tls.VersionTLS13 || request.Header.Get("Authorization") != "Bearer synthetic-ssh-material" {
		fixture.status(writer, http.StatusForbidden, metav1.StatusReasonForbidden)
		return
	}
	collection := "/api/v1/namespaces/" + fixture.provision.Namespace + "/secrets"
	item := collection + "/" + value.SSHIdentitySecretName(fixture.provision.IdentityRevisionRef, fixture.provision.Revision)
	if request.URL.Path == "/redirected" {
		fixture.redirectHits.Add(1)
		fixture.status(writer, http.StatusBadRequest, metav1.StatusReasonBadRequest)
		return
	}
	if fixture.redirectCode != 0 && request.Method == fixture.redirectVerb {
		switch request.Method {
		case http.MethodGet:
			fixture.gets.Add(1)
		case http.MethodPost:
			fixture.posts.Add(1)
		case http.MethodDelete:
			fixture.deletes.Add(1)
		}
		writer.Header().Set("Location", "/redirected")
		fixture.status(writer, fixture.redirectCode, metav1.StatusReasonUnknown)
		return
	}
	switch {
	case request.Method == http.MethodPost && request.URL.Path == collection:
		fixture.posts.Add(1)
		if fixture.createStatus != 0 {
			writer.Header().Set("Retry-After", "0")
			if fixture.echoBody {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(fixture.createStatus)
				_, _ = io.Copy(writer, io.LimitReader(request.Body, 65537))
				return
			}
			fixture.status(writer, fixture.createStatus, metav1.StatusReasonUnknown)
			return
		}
		var secret corev1.Secret
		if json.NewDecoder(io.LimitReader(request.Body, 65537)).Decode(&secret) != nil {
			fixture.status(writer, http.StatusBadRequest, metav1.StatusReasonBadRequest)
			return
		}
		secret.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}
		secret.UID, secret.ResourceVersion = "wire-uid-synthetic", "opaque-wire-rv"
		fixture.secret = secret.DeepCopy()
		if fixture.createDrop {
			connection, _, err := writer.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close()
			}
			return
		}
		if fixture.createReadDrop {
			writer.Header().Set("Content-Length", "1024")
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, "{")
			return
		}
		if fixture.createACK == "wrong content type" {
			writer.Header().Set("Content-Type", "text/plain")
		} else {
			writer.Header().Set("Content-Type", "application/json")
		}
		if fixture.createACK == "wrong success status" {
			writer.WriteHeader(http.StatusOK)
		} else {
			writer.WriteHeader(http.StatusCreated)
		}
		switch fixture.createACK {
		case "malformed":
			_, _ = io.WriteString(writer, "{")
		case "empty":
		case "missing UID":
			secret.UID = ""
			_ = json.NewEncoder(writer).Encode(&secret)
		case "wrong tuple":
			secret.Annotations[sshIdentityTargetAnnotation] = "foreign-target"
			_ = json.NewEncoder(writer).Encode(&secret)
		case "wrong kind":
			secret.Kind = "ConfigMap"
			_ = json.NewEncoder(writer).Encode(&secret)
		case "duplicate UID":
			_, _ = io.WriteString(writer, `{"apiVersion":"v1","kind":"Secret","metadata":{"uid":"first","uid":"second"}}`)
		case "oversized":
			_, _ = io.WriteString(writer, strings.Repeat(" ", 65538))
		default:
			_ = json.NewEncoder(writer).Encode(&secret)
		}
	case request.Method == http.MethodGet && request.URL.Path == item:
		fixture.gets.Add(1)
		if fixture.getStatus != 0 {
			fixture.status(writer, fixture.getStatus, metav1.StatusReasonUnknown)
			return
		}
		if fixture.secret == nil {
			fixture.status(writer, http.StatusNotFound, metav1.StatusReasonNotFound)
			return
		}
		if fixture.getReadDrop {
			writer.Header().Set("Content-Length", "1024")
			writer.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(writer, "{")
			return
		}
		if fixture.getACK == "wrong content type" {
			writer.Header().Set("Content-Type", "text/plain")
		} else {
			writer.Header().Set("Content-Type", "application/json")
		}
		if fixture.getACK == "wrong success status" {
			writer.WriteHeader(http.StatusCreated)
		}
		switch fixture.getACK {
		case "malformed":
			_, _ = io.WriteString(writer, "{")
			return
		case "oversized":
			_, _ = io.WriteString(writer, strings.Repeat(" ", 65538))
			return
		case "duplicate UID":
			_, _ = io.WriteString(writer, `{"apiVersion":"v1","kind":"Secret","metadata":{"uid":"first","uid":"second"}}`)
			return
		case "unknown field":
			wire, _ := json.Marshal(fixture.secret)
			defer clear(wire)
			wire = append([]byte(`{"futureField":true,`), wire[1:]...)
			_, _ = writer.Write(wire)
			return
		}
		_ = json.NewEncoder(writer).Encode(fixture.secret)
	case request.Method == http.MethodDelete && request.URL.Path == item:
		fixture.deletes.Add(1)
		if json.NewDecoder(io.LimitReader(request.Body, 4097)).Decode(&fixture.lastDelete) != nil || fixture.lastDelete.Preconditions == nil || fixture.lastDelete.Preconditions.UID == nil || fixture.lastDelete.Preconditions.ResourceVersion == nil {
			fixture.status(writer, http.StatusBadRequest, metav1.StatusReasonBadRequest)
			return
		}
		if fixture.deleteStatus != 0 {
			writer.Header().Set("Retry-After", "0")
			fixture.status(writer, fixture.deleteStatus, metav1.StatusReasonUnknown)
			return
		}
		if fixture.secret == nil || string(*fixture.lastDelete.Preconditions.UID) != string(fixture.secret.UID) || *fixture.lastDelete.Preconditions.ResourceVersion != fixture.secret.ResourceVersion {
			fixture.status(writer, http.StatusConflict, metav1.StatusReasonConflict)
			return
		}
		if !fixture.deleteRetain {
			fixture.secret = nil
		}
		fixture.status(writer, http.StatusOK, "")
	default:
		fixture.status(writer, http.StatusNotFound, metav1.StatusReasonNotFound)
	}
}

// status emits a real Kubernetes Status envelope without echoing any private fixture metadata, allowing the actual SDK retry/error paths to run.
// status выводит настоящий Kubernetes Status envelope без private fixture metadata для выполнения actual SDK retry/error paths.
func (fixture *sshMaterialWire) status(writer http.ResponseWriter, code int, reason metav1.StatusReason) {
	state := metav1.StatusFailure
	if code < 400 {
		state = metav1.StatusSuccess
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(code)
	_ = json.NewEncoder(writer).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: state, Reason: reason, Code: int32(code), Message: "synthetic purpose API status"})
}

// TestSSHObservedSuccessRequiresTerminalEvidence separates fully received invalid GET responses from interrupted transport and non-success API reads through actual certificate-verified TLS.
// TestSSHObservedSuccessRequiresTerminalEvidence разделяет fully received invalid GET responses, interrupted transport и non-success API reads через actual certificate-verified TLS.
func TestSSHObservedSuccessRequiresTerminalEvidence(t *testing.T) {
	for _, mode := range []string{"malformed", "oversized", "duplicate UID", "unknown field", "wrong content type", "wrong success status", "wrong tuple", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			store, wire, _ := newSSHMaterialWire(t)
			_, secret := sshMaterialFixture()
			wire.secret = secret.DeepCopy()
			wire.getACK = mode
			if mode == "wrong tuple" {
				wire.secret.Annotations[sshIdentityTargetAnnotation] = "foreign-target"
			}
			wire.getReadDrop = mode == "interrupted"
			locator, err := store.ObserveSSHIdentity(t.Context(), wire.provision)
			if wire.gets.Load() != 1 || wire.posts.Load() != 0 || locator.Valid() {
				t.Fatal("observation changed private single-read boundary")
			}
			if mode == "interrupted" {
				if !errors.Is(err, sshrepository.ErrIdentityUnavailable) || errors.Is(err, sshrepository.ErrIdentityAcknowledgementRejected) {
					t.Fatal("interrupted read became a completed acknowledgement")
				}
				return
			}
			var witness *sshrepository.IdentityAcknowledgementRejection
			if !errors.As(err, &witness) || errors.Is(err, sshrepository.ErrIdentityUnknown) || len(witness.ResponseSHA256) != 64 {
				t.Fatal("completed invalid observation stayed recoverable")
			}
			if (mode == "unknown field" || mode == "wrong tuple") && witness.ObservedUID != string(secret.UID) {
				t.Fatal("unambiguous observed UID was lost before semantic validation")
			}
			if mode == "duplicate UID" && witness.ObservedUID != "" {
				t.Fatal("ambiguous observed UID was retained")
			}
		})
	}
}

// TestSSHPrivateReadPreservesDependencyFailures prevents Kubernetes availability, absence and caller cancellation from being reported as a credential or provider authorization rejection.
// TestSSHPrivateReadPreservesDependencyFailures не позволяет Kubernetes availability, absence и caller cancellation стать credential либо provider authorization rejection.
func TestSSHPrivateReadPreservesDependencyFailures(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusForbidden, http.StatusNotFound} {
		store, wire, _ := newSSHMaterialWire(t)
		locator, secret := sshMaterialFixture()
		wire.secret = secret
		wire.getStatus = status
		raw, err := store.ReadSSHIdentity(t.Context(), locator)
		clear(raw)
		expected := sshrepository.ErrIdentityUnavailable
		if status == http.StatusNotFound {
			expected = sshrepository.ErrIdentityAbsent
		}
		if !errors.Is(err, expected) || errors.Is(err, sshrepository.ErrIdentityRejected) || wire.gets.Load() != 1 || wire.posts.Load() != 0 {
			t.Fatal("private API dependency failure became credential rejection")
		}
	}
	store, wire, _ := newSSHMaterialWire(t)
	locator, _ := sshMaterialFixture()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if raw, err := store.ReadSSHIdentity(ctx, locator); len(raw) != 0 || !errors.Is(err, sshrepository.ErrIdentityUnavailable) || wire.gets.Load() != 0 {
		clear(raw)
		t.Fatal("cancelled private read changed authentication evidence")
	}
}

// TestSSHMaterialCreateReadDeleteUsesActualTLSAndExactPins drives the real SDK producer, private reader and exact delete-precondition consumer through a local TLS API rather than a fake-client-only schema test.
// TestSSHMaterialCreateReadDeleteUsesActualTLSAndExactPins проводит real SDK producer, private reader и exact delete-precondition consumer через local TLS API, а не fake-client-only schema test.
func TestSSHMaterialCreateReadDeleteUsesActualTLSAndExactPins(t *testing.T) {
	store, fixture, material := newSSHMaterialWire(t)
	locator, err := store.CreateSSHIdentity(t.Context(), fixture.provision, material)
	if err != nil || !locator.Valid() || fixture.posts.Load() != 1 || fixture.gets.Load() != 0 {
		t.Fatal("one immutable SSH material create failed")
	}
	read, err := store.ReadSSHIdentity(t.Context(), locator)
	if err != nil || string(read) != string(material) {
		t.Fatal("exact bound SSH purpose read failed")
	}
	clear(read)
	if err := store.DeleteSSHIdentity(t.Context(), locator); err != nil || fixture.deletes.Load() != 1 {
		t.Fatal("exact SSH purpose cleanup failed")
	}
	fixture.mutex.Lock()
	defer fixture.mutex.Unlock()
	if fixture.lastDelete.Preconditions == nil || fixture.lastDelete.Preconditions.UID == nil || fixture.lastDelete.Preconditions.ResourceVersion == nil || string(*fixture.lastDelete.Preconditions.UID) != locator.SecretUID || *fixture.lastDelete.Preconditions.ResourceVersion != locator.SecretResourceVersion || fixture.secret != nil {
		t.Fatal("cleanup lost immutable object pins or absence proof")
	}
}

// TestSSHMaterialUnknownCreateNeverRetriesOrImplicitlyObserves proves 429/5xx with Retry-After, conflict and lost acknowledgement produce one POST and zero implicit GETs; absence remains an observation only.
// TestSSHMaterialUnknownCreateNeverRetriesOrImplicitlyObserves доказывает один POST и отсутствие implicit GET при429/5xx с Retry-After, conflict и lost acknowledgement; absence остаётся только observation.
func TestSSHMaterialUnknownCreateNeverRetriesOrImplicitlyObserves(t *testing.T) {
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusConflict, 0} {
		store, fixture, material := newSSHMaterialWire(t)
		fixture.mutex.Lock()
		fixture.createStatus, fixture.createDrop = code, code == 0
		fixture.mutex.Unlock()
		locator, err := store.CreateSSHIdentity(t.Context(), fixture.provision, material)
		if !errors.Is(err, sshrepository.ErrIdentityUnknown) || err.Error() != sshrepository.ErrIdentityUnknown.Error() || locator.Valid() || fixture.posts.Load() != 1 || fixture.gets.Load() != 0 {
			t.Fatal("ambiguous create retried, observed implicitly or leaked diagnostics", code)
		}
		observed, err := store.ObserveSSHIdentity(t.Context(), fixture.provision)
		if code == 0 && (err != nil || !observed.Valid()) || code != 0 && !errors.Is(err, sshrepository.ErrIdentityAbsent) || fixture.posts.Load() != 1 || fixture.gets.Load() != 1 {
			t.Fatal("explicit read-only reconciliation changed the write intent", code)
		}
	}
}

// TestSSHMaterialDeleteNeverRetriesOrClaimsUnjoinedAbsence rejects stale object pins before DELETE and retains UNKNOWN after a throttled write or acknowledged delete with a still-present object.
// TestSSHMaterialDeleteNeverRetriesOrClaimsUnjoinedAbsence отклоняет stale object pins до DELETE и сохраняет UNKNOWN после throttled write либо acknowledged delete с оставшимся object.
func TestSSHMaterialDeleteNeverRetriesOrClaimsUnjoinedAbsence(t *testing.T) {
	for _, mode := range []string{"stale UID", "stale RV", "429", "retained"} {
		store, fixture, material := newSSHMaterialWire(t)
		locator, err := store.CreateSSHIdentity(t.Context(), fixture.provision, material)
		if err != nil {
			t.Fatal(err)
		}
		if mode == "stale UID" {
			locator.SecretUID = "another-uid"
		} else if mode == "stale RV" {
			locator.SecretResourceVersion = "another-rv"
		} else if mode == "429" {
			fixture.mutex.Lock()
			fixture.deleteStatus = http.StatusTooManyRequests
			fixture.mutex.Unlock()
		} else {
			fixture.mutex.Lock()
			fixture.deleteRetain = true
			fixture.mutex.Unlock()
		}
		err = store.DeleteSSHIdentity(t.Context(), locator)
		if strings.HasPrefix(mode, "stale") {
			if !errors.Is(err, sshrepository.ErrIdentityRejected) || fixture.deletes.Load() != 0 {
				t.Fatal("stale object pins reached delete")
			}
		} else if !errors.Is(err, sshrepository.ErrIdentityUnknown) || fixture.deletes.Load() != 1 {
			t.Fatal("cleanup retried or claimed unproved absence")
		}
	}
}

// TestSSHMaterialRedirectNeverReplaysPrivateBodyOrCredential checks every ordinary redirect status for GET/POST/DELETE through the real TLS SDK while leaving the original shared HTTP client unchanged.
// TestSSHMaterialRedirectNeverReplaysPrivateBodyOrCredential проверяет все ordinary redirect statuses для GET/POST/DELETE через real TLS SDK без изменения original shared HTTP client.
func TestSSHMaterialRedirectNeverReplaysPrivateBodyOrCredential(t *testing.T) {
	for _, code := range []int{301, 302, 303, 307, 308} {
		for _, verb := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			store, fixture, material := newSSHMaterialWire(t)
			var locator value.SSHIdentityLocator
			var err error
			if verb != http.MethodPost {
				locator, err = store.CreateSSHIdentity(t.Context(), fixture.provision, material)
				if err != nil {
					t.Fatal("redirect fixture seed failed")
				}
			}
			original := store.client.CoreV1().RESTClient().(*rest.RESTClient)
			isolated, err := store.sshIdentityRESTClient()
			if err != nil || isolated.GetRateLimiter() != original.GetRateLimiter() || original.Client.CheckRedirect != nil {
				t.Fatal("purpose transport copied SDK state or changed the shared client")
			}
			fixture.mutex.Lock()
			fixture.redirectCode, fixture.redirectVerb = code, verb
			fixture.mutex.Unlock()
			switch verb {
			case http.MethodGet:
				_, err = store.ReadSSHIdentity(t.Context(), locator)
			case http.MethodPost:
				_, err = store.CreateSSHIdentity(t.Context(), fixture.provision, material)
			case http.MethodDelete:
				err = store.DeleteSSHIdentity(t.Context(), locator)
			}
			attempts := map[string]int32{http.MethodGet: fixture.gets.Load(), http.MethodPost: fixture.posts.Load(), http.MethodDelete: fixture.deletes.Load()}
			if err == nil || attempts[verb] != 1 || fixture.redirectHits.Load() != 0 || original.Client.CheckRedirect != nil {
				t.Fatal("purpose redirect crossed the exact credential recipient", code, verb)
			}
		}
	}
}

// TestSSHMaterialCompletedInvalidACKIsTerminalReject proves damaged success responses cannot enter the lost-ACK observation path; one bounded private witness survives without a usable locator or an implicit second API request.
// TestSSHMaterialCompletedInvalidACKIsTerminalReject доказывает, что damaged success responses не попадают в lost-ACK observation path; один bounded private witness сохраняется без usable locator либо implicit второго API request.
func TestSSHMaterialCompletedInvalidACKIsTerminalReject(t *testing.T) {
	for _, mode := range []string{"malformed", "empty", "missing UID", "wrong tuple", "wrong kind", "duplicate UID", "oversized", "wrong content type", "wrong success status"} {
		t.Run(mode, func(t *testing.T) {
			store, fixture, material := newSSHMaterialWire(t)
			fixture.createACK = mode
			locator, err := store.CreateSSHIdentity(t.Context(), fixture.provision, material)
			var receipt *sshrepository.IdentityAcknowledgementRejection
			if !errors.Is(err, sshrepository.ErrIdentityAcknowledgementRejected) || errors.Is(err, sshrepository.ErrIdentityUnknown) || !errors.As(err, &receipt) || locator.Valid() || fixture.posts.Load() != 1 || fixture.gets.Load() != 0 {
				t.Fatal("damaged successful acknowledgement entered reconciliation or lost its terminal receipt")
			}
			if err.Error() != sshrepository.ErrIdentityAcknowledgementRejected.Error() || len(receipt.ResponseSHA256) != 64 || receipt.Truncated != (mode == "oversized") {
				t.Fatal("private acknowledgement witness or fixed diagnostic changed")
			}
			// An exact metadata UID remains evidence even when kind is wrong; it never becomes a usable Secret locator.
			// Exact metadata UID остаётся evidence при неверном kind, но никогда не становится usable Secret locator.
			if (mode == "wrong tuple" || mode == "wrong kind" || mode == "wrong content type" || mode == "wrong success status") != (receipt.ObservedUID == "wire-uid-synthetic") {
				t.Fatal("unambiguous acknowledged UID was lost or ambiguous body invented a UID")
			}
		})
	}
}

// TestSSHMaterialIncompleteSuccessfulBodyStaysUnknown separates an interrupted HTTP body from a completed damaged ACK; only the former may use an independently admitted observation of the original intent.
// TestSSHMaterialIncompleteSuccessfulBodyStaysUnknown отделяет interrupted HTTP body от completed damaged ACK; только первый допускает отдельно разрешённое observation исходного intent.
func TestSSHMaterialIncompleteSuccessfulBodyStaysUnknown(t *testing.T) {
	store, fixture, material := newSSHMaterialWire(t)
	fixture.createReadDrop = true
	locator, err := store.CreateSSHIdentity(t.Context(), fixture.provision, material)
	if !errors.Is(err, sshrepository.ErrIdentityUnknown) || errors.Is(err, sshrepository.ErrIdentityAcknowledgementRejected) || locator.Valid() || fixture.posts.Load() != 1 || fixture.gets.Load() != 0 {
		t.Fatal("interrupted success body was accepted, retried or reclassified as completed rejection")
	}
	observed, err := store.ObserveSSHIdentity(t.Context(), fixture.provision)
	if err != nil || !observed.Valid() || fixture.posts.Load() != 1 || fixture.gets.Load() != 1 {
		t.Fatal("separately admitted lost acknowledgement observation failed")
	}
}

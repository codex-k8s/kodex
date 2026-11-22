package kubernetestarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// watchPreparationFixture connects real selected-identity discovery to a structural owner collection proposal, without manufacturing object pins.
// watchPreparationFixture соединяет real selected-identity discovery со structural owner collection proposal без выдуманных object pins.
func watchPreparationFixture(t *testing.T, all bool, handler http.HandlerFunc) (*PodStreamPreparer, value.KubernetesOperationSnapshot, targetrepo.OperationChild, *streamMaterialFixture, *resourceWireFixture) {
	t.Helper()
	wire := &resourceWireFixture{streamHandler: handler}
	credential := resourceTLSFixture(t, wire)
	reader := &streamMaterialFixture{raw: streamCredentialBytes(t, credential)}
	t.Cleanup(func() { clear(reader.raw) })
	locator := identityLocatorFixture(t, reader.raw, credential.target)
	input, _, operation := watchInputFixture("configmaps", all)
	input.Options = json.RawMessage(`{"timeoutSeconds":30,"allowWatchBookmarks":true}`)
	raw, err := managedkubernetes.EncodeOperationInput(operation, input)
	if err != nil {
		t.Fatal(err)
	}
	_, canonical, err := managedkubernetes.DecodeOperationInput(operation, raw)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := value.KubernetesOperationSnapshot{Digest: strings.Repeat("a", 64), Operation: operation, Identity: locator, IdentityRef: locator.IdentityRef, IdentityGeneration: 1, SubjectDigest: subjectPin(t), Target: credential.target, Input: input, NormalizedInput: canonical, ExpiresAt: time.Now().Add(time.Minute), ProtectedNamespaces: []string{"private"}}
	factory, err := NewPodStreamPreparer(reader, locator.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := factory.ObserveKubernetesPod(t.Context(), snapshot)
	if err != nil {
		t.Fatal("observe collection", err)
	}
	digest := sha256.Sum256(canonical)
	child := targetrepo.OperationChild{Ref: "child_synthetic", Operation: operation, InputDigest: hex.EncodeToString(digest[:]), State: "READY", Resource: observed.Resource, Input: input, NormalizedInput: append([]byte(nil), canonical...), Observation: &observed}
	return factory, snapshot, child, reader, wire
}

// TestWatchPreparationUsesCollectionAndOneNativeHandoff exercises actual Prepare/Open with fresh native reviews, immutable schema and owner channels.
// TestWatchPreparationUsesCollectionAndOneNativeHandoff проверяет actual Prepare/Open со fresh native reviews, immutable schema и owner channels.
func TestWatchPreparationUsesCollectionAndOneNativeHandoff(t *testing.T) {
	for _, all := range []bool{false, true} {
		factory, snapshot, child, _, wire := watchPreparationFixture(t, all, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "ADDED", "object": watchObjectFixture("ConfigMap", "example")})
		})
		if child.Observation.ObservationState != managedkubernetes.ObservationCollection || child.Observation.PodWitness != nil || child.Resource.Name != "" || child.Resource.UID != "" || child.Resource.AllNamespaces != all || wire.streamCalls.Load() != 0 || wire.reads.Load() != 0 {
			t.Fatal("watch observation invented GET/object pins")
		}
		prepared, err := factory.PrepareKubernetesPodStream(t.Context(), snapshot, child)
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Close()
		binding, admission := podStreamAdmissionFixture(child)
		admission.Stream.OutputChannels = []string{"STDOUT"}
		attempt, cancel := context.WithCancel(t.Context())
		stream, receipt, err := prepared.Open(t.Context(), attempt, binding, admission)
		cancel()
		prepared.Close()
		if err != nil {
			t.Fatal("open admitted watch", err)
		}
		defer stream.Close()
		if receipt.Protocol != managedkubernetes.HTTPWatchV1 || receipt.TransportRef != snapshot.Target.ID || receipt.Validate() != nil {
			t.Fatal("watch lost API receipt")
		}
		read, err := stream.ReadStream()
		if err != nil || len(read.Payload) == 0 || strings.Contains(string(read.Payload), "private-snapshot") {
			t.Fatal("watch lost sanitized handoff", err)
		}
		if _, err := prepared.TargetAccess().VerifyKubernetesStreamAccess(t.Context(), snapshot); err != nil {
			t.Fatal("fresh frame authorization", err)
		}
		if duplicate, _, err := prepared.Open(t.Context(), t.Context(), binding, admission); err == nil || duplicate != nil || wire.streamCalls.Load() != 1 {
			t.Fatal("watch preparation reopened")
		}
	}
}

// TestWatchAccessRevocationAndSchemaDriftCloseOutput tests the actual native authorization path after initial preparation without new watch effects.
// TestWatchAccessRevocationAndSchemaDriftCloseOutput проверяет actual native authorization после initial preparation без новых watch effects.
func TestWatchAccessRevocationAndSchemaDriftCloseOutput(t *testing.T) {
	for _, variant := range []string{"permission", "credential", "schema", "identity", "target", "protected scope", "collection"} {
		t.Run(variant, func(t *testing.T) {
			factory, snapshot, child, reader, wire := watchPreparationFixture(t, false, nil)
			prepared, err := factory.PrepareKubernetesPodStream(t.Context(), snapshot, child)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if _, err := prepared.TargetAccess().VerifyKubernetesStreamAccess(t.Context(), snapshot); err != nil {
				t.Fatal(err)
			}
			switch variant {
			case "permission":
				wire.deny.Store(true)
			case "credential":
				reader.corrupt.Store(true)
			case "schema":
				wire.changeSchema.Store(true)
			case "identity":
				snapshot.IdentityRef = "foreign"
			case "target":
				snapshot.Target.Revision++
			case "protected scope":
				snapshot.ProtectedNamespaces = append(snapshot.ProtectedNamespaces, "example")
			case "collection":
				child.Resource.AllNamespaces = true
				child.Resource.Namespace = ""
			}
			if variant == "collection" {
				if replacement, err := factory.PrepareKubernetesPodStream(t.Context(), snapshot, child); err == nil || replacement != nil {
					t.Fatal("child widened collection")
				}
			} else if until, err := prepared.TargetAccess().VerifyKubernetesStreamAccess(t.Context(), snapshot); err == nil || !until.IsZero() {
				t.Fatal("revoked watch retained output credit")
			}
			if wire.streamCalls.Load() != 0 {
				t.Fatal("access check opened watch")
			}
		})
	}
}

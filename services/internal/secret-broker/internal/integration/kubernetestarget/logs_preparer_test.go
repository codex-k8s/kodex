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

// logsPreparationFixture adapts the native discovery fixture to a nonprivileged HTTP owner proposal.
// logsPreparationFixture переводит native discovery fixture в nonprivileged HTTP owner proposal.
func logsPreparationFixture(t *testing.T, handler http.HandlerFunc) (*PodStreamPreparer, value.KubernetesOperationSnapshot, targetrepo.OperationChild, *streamMaterialFixture, *resourceWireFixture) {
	t.Helper()
	factory, snapshot, child, reader, wire := podStreamPreparationFixture(t, handler)
	snapshot.Operation = "kubernetes.logs"
	snapshot.Input.Options = json.RawMessage(`{"follow":true,"timeoutSeconds":30}`)
	raw, err := managedkubernetes.EncodeOperationInput(snapshot.Operation, snapshot.Input)
	if err != nil {
		t.Fatal(err)
	}
	_, snapshot.NormalizedInput, err = managedkubernetes.DecodeOperationInput(snapshot.Operation, raw)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := factory.ObserveKubernetesPod(t.Context(), snapshot)
	if err != nil || observed.PodWitness != nil {
		t.Fatal("logs observation manufactured privileged transport", err)
	}
	digest := sha256.Sum256(snapshot.NormalizedInput)
	child.Operation, child.Input, child.NormalizedInput, child.InputDigest = snapshot.Operation, snapshot.Input, snapshot.NormalizedInput, hex.EncodeToString(digest[:])
	child.NodeTransport, child.Observation, child.Resource = nil, &observed, observed.Resource
	return factory, snapshot, child, reader, wire
}

// TestLogsPreparationTransfersAPIStream verifies exact selection, one opening, detached unary lifetime and clean HTTP completion.
// TestLogsPreparationTransfersAPIStream проверяет exact selection, одно открытие, отдельный unary lifetime и clean HTTP completion.
func TestLogsPreparationTransfersAPIStream(t *testing.T) {
	factory, snapshot, child, _, wire := logsPreparationFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Query().Get("container") != "first" || r.URL.Query().Get("follow") != "true" {
			t.Error("log transport changed exact Pod/container route")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("synthetic log bytes"))
	})
	prepared, err := factory.PrepareKubernetesPodStream(t.Context(), snapshot, child)
	if err != nil {
		t.Fatal("prepare logs", err)
	}
	defer prepared.Close()
	metadata, valid := prepared.Binding()
	if !valid || metadata.Observation.PodWitness != nil || wire.streamCalls.Load() != 0 {
		t.Fatal("invalid logs preparation")
	}
	binding, admission := podStreamAdmissionFixture(child)
	admission.Stream.OutputChannels = []string{"STDOUT"}
	attempt, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream, receipt, err := prepared.Open(t.Context(), attempt, binding, admission)
	if err != nil {
		t.Fatal("open logs", err)
	}
	defer stream.Close()
	cancel()
	prepared.Close()
	if receipt.Validate() != nil || receipt.Protocol != managedkubernetes.HTTPLogsV1 || receipt.TransportRef != snapshot.Target.ID {
		t.Fatal("invalid API receipt")
	}
	var result []byte
	for {
		read, err := stream.ReadStream()
		if err != nil {
			t.Fatal("read detached logs", err)
		}
		result = append(result, read.Payload...)
		clear(read.Payload)
		if read.CleanEOF {
			break
		}
	}
	if string(result) != "synthetic log bytes" || wire.streamCalls.Load() != 1 {
		t.Fatal("lost or repeated log request")
	}
	if duplicate, _, err := prepared.Open(t.Context(), t.Context(), binding, admission); err == nil || duplicate != nil {
		t.Fatal("reopened consumed logs preparation")
	}
}

// TestLogsAccessRejectsCurrentRevocationAndReplacement proves fresh target checks do not retain the opening authority.
// TestLogsAccessRejectsCurrentRevocationAndReplacement доказывает, что fresh target checks не сохраняют opening authority.
func TestLogsAccessRejectsCurrentRevocationAndReplacement(t *testing.T) {
	for _, variant := range []string{"permission", "credential", "Pod UID", "container", "identity", "target", "resource version"} {
		t.Run(variant, func(t *testing.T) {
			factory, snapshot, child, reader, wire := logsPreparationFixture(t, nil)
			prepared, err := factory.PrepareKubernetesPodStream(t.Context(), snapshot, child)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			access := prepared.TargetAccess()
			if _, err := access.VerifyKubernetesStreamAccess(t.Context(), snapshot); err != nil {
				t.Fatal("initial access", err)
			}
			switch variant {
			case "permission":
				wire.deny.Store(true)
			case "credential":
				reader.corrupt.Store(true)
			case "Pod UID":
				wire.podBody = strings.Replace(wire.podBody, child.Resource.UID, "22222222-2222-4222-8222-222222222222", 1)
			case "container":
				wire.podBody = strings.ReplaceAll(wire.podBody, "first", "renamed")
			case "identity":
				snapshot.IdentityRef = "foreign"
			case "target":
				snapshot.Target.Revision++
			case "resource version":
				wire.podBody = strings.Replace(wire.podBody, `"resourceVersion":"10"`, `"resourceVersion":"11"`, 1)
			}
			until, err := access.VerifyKubernetesStreamAccess(t.Context(), snapshot)
			if variant == "resource version" {
				if err != nil || !until.After(time.Now()) {
					t.Fatal("normal mutable Pod status invalidated logs")
				}
			} else if err == nil || !until.IsZero() {
				t.Fatal("changed authority retained output credit")
			}
			if wire.streamCalls.Load() != 0 {
				t.Fatal("access check opened a stream")
			}
		})
	}
}

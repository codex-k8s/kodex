package kubernetestarget

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	listvalidation "k8s.io/apimachinery/pkg/apis/meta/internalversion/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestWatchRequestFalseInitialEventsUsesUpstreamValidation preserves explicit false and matches the actual pinned Kubernetes validator.
// TestWatchRequestFalseInitialEventsUsesUpstreamValidation сохраняет explicit false и соответствует actual pinned Kubernetes validator.
func TestWatchRequestFalseInitialEventsUsesUpstreamValidation(t *testing.T) {
	credential, calls := transportFixture(t, false)
	input, address, operation := watchInputFixture("configmaps", false)
	input.Options = json.RawMessage(`{"timeoutSeconds":10,"sendInitialEvents":false,"fieldSelector":"metadata.name=selected"}`)
	request, _, err := credential.watchRequest(t.Context(), operation, input, address)
	if err != nil {
		t.Fatal(err)
	}
	q := request.URL.Query()
	if q.Get("sendInitialEvents") != "false" || q.Get("fieldSelector") != "metadata.name=selected" || q.Get("resourceVersionMatch") != "NotOlderThan" || calls.Load() != 0 {
		t.Fatal("typed watch query changed")
	}
	initial := false
	options := internalversion.ListOptions{Watch: true, SendInitialEvents: &initial, ResourceVersionMatch: metav1.ResourceVersionMatch(q.Get("resourceVersionMatch"))}
	if len(listvalidation.ValidateListOptions(&options, true)) != 0 {
		t.Fatal("wire watch query violates pinned upstream contract")
	}
}

// TestNativeWatchRejectsWrongMediaBeforeReading refuses an unqualified protocol response instead of parsing arbitrary content as watch JSON.
// TestNativeWatchRejectsWrongMediaBeforeReading отвергает неподтверждённый protocol response вместо разбора arbitrary content как watch JSON.
func TestNativeWatchRejectsWrongMediaBeforeReading(t *testing.T) {
	credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(`{"type":"BOOKMARK"}`))
	}))
	input, address, operation := watchInputFixture("configmaps", false)
	now := time.Now()
	stream, err := credential.openWatchHTTP(t.Context(), t.Context(), operation, input, address, watchFixtureSchema(t, "ConfigMap"), nil, now.Add(time.Second), now.Add(time.Second), 4096)
	if err == nil || stream != nil || calls.Load() != 1 {
		t.Fatal("wrong watch media was accepted or retried")
	}
}

// watchFixtureSchema compiles actual private OpenAPI bytes, including metadata fields that must be stripped before output.
// watchFixtureSchema компилирует actual private OpenAPI bytes с metadata fields, удаляемыми до output.
func watchFixtureSchema(t *testing.T, kind string) *registeredResourceSchema {
	t.Helper()
	document := resourceSchemaFixture(t)
	models := document["components"].(map[string]any)["schemas"].(map[string]any)
	model := models["ConfigMap"].(map[string]any)
	model["x-kubernetes-group-version-kind"] = []any{map[string]any{"group": "", "version": "v1", "kind": kind}}
	meta := models["Meta"].(map[string]any)["properties"].(map[string]any)
	meta["annotations"] = map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
	properties := model["properties"].(map[string]any)
	properties["type"] = map[string]any{"type": "string"}
	properties["immutable"] = map[string]any{"type": "boolean"}
	properties["stringData"] = properties["data"]
	raw, _ := json.Marshal(document)
	sum := sha512.Sum512(raw)
	registered, err := compileRegisteredResourceSchema(raw, schema.GroupVersionKind{Version: "v1", Kind: kind}, strings.ToUpper(hex.EncodeToString(sum[:])))
	if err != nil {
		t.Fatal(err)
	}
	return registered
}

// watchObjectFixture builds a versioned synthetic event whose private values must not survive Secret projection.
// watchObjectFixture строит versioned synthetic event; private values не должны пройти Secret projection.
func watchObjectFixture(kind, namespace string) map[string]any {
	return map[string]any{"apiVersion": "v1", "kind": kind, "metadata": map[string]any{"name": "selected", "namespace": namespace, "uid": "11111111-1111-4111-8111-111111111111", "resourceVersion": "17", "annotations": map[string]any{"kubectl.kubernetes.io/last-applied-configuration": "private-snapshot"}}, "data": map[string]any{"key": "cHJpdmF0ZS12YWx1ZQ=="}}
}

// watchInputFixture pins a collection and explicit initial-events/bookmark options without granting access.
// watchInputFixture закрепляет collection и explicit initial-events/bookmark options без выдачи access.
func watchInputFixture(resource string, all bool) (managedkubernetes.OperationInput, ResourceAddress, string) {
	input := managedkubernetes.OperationInput{Version: "v1", Resource: resource, Namespace: "example", Selector: "app=example", Options: json.RawMessage(`{"timeoutSeconds":10,"sendInitialEvents":true,"allowWatchBookmarks":true,"resourceVersion":"17"}`)}
	address := ResourceAddress{Version: "v1", Resource: resource, Namespace: "example", Namespaced: true}
	operation := "kubernetes.watch"
	if all {
		input.Namespace = ""
		address.Namespace = ""
		address.AllNamespaces = true
		operation = "kubernetes.watch.all-namespaces"
	}
	return input, address, operation
}

// TestNativeWatchHTTPProjectsBeforeFraming proves real TLS streaming, exact query, all-namespace filtering, Secret redaction and unary handoff.
// TestNativeWatchHTTPProjectsBeforeFraming доказывает real TLS streaming, exact query, all-namespace filtering, Secret redaction и unary handoff.
func TestNativeWatchHTTPProjectsBeforeFraming(t *testing.T) {
	for _, certificate := range []bool{false, true} {
		t.Run(map[bool]string{false: "token", true: "certificate"}[certificate], func(t *testing.T) {
			registered := watchFixtureSchema(t, "Secret")
			credential, calls := transportFixtureWithHandler(t, certificate, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if r.Method != "GET" || r.URL.Path != "/api/v1/secrets" || q.Get("watch") != "true" || q.Get("timeoutSeconds") != "10" || q.Get("resourceVersion") != "17" || q.Get("resourceVersionMatch") != "NotOlderThan" || q.Get("labelSelector") != "app=example" || q.Get("sendInitialEvents") != "true" || q.Get("allowWatchBookmarks") != "true" || len(q) != 7 || r.Header.Get("Accept") != "application/json" {
					t.Error("watch changed typed route")
					w.WriteHeader(403)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				encoder := json.NewEncoder(w)
				_ = encoder.Encode(map[string]any{"type": "ADDED", "object": watchObjectFixture("Secret", "private")})
				for _, kind := range []string{"ADDED", "MODIFIED", "DELETED"} {
					_ = encoder.Encode(map[string]any{"type": kind, "object": watchObjectFixture("Secret", "example")})
				}
				_ = encoder.Encode(map[string]any{"type": "BOOKMARK", "object": map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"resourceVersion": "18", "annotations": map[string]any{"private": "must-not-escape", "k8s.io/initial-events-end": "true"}}, "data": map[string]any{"private": "must-not-escape"}}})
			}))
			input, address, operation := watchInputFixture("secrets", true)
			attempt, cancel := context.WithCancel(t.Context())
			now := time.Now()
			stream, err := credential.openWatchHTTP(t.Context(), attempt, operation, input, address, registered, []string{"private"}, now.Add(3*time.Second), now.Add(time.Second), 128<<10)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			var received bytes.Buffer
			for {
				read, err := stream.ReadStream()
				if err != nil {
					t.Fatal(err)
				}
				if read.CleanEOF {
					break
				}
				if read.Channel != "STDOUT" || len(read.Payload) > managedkubernetes.MaximumFrameBytes {
					t.Fatal("unbounded watch frame")
				}
				received.Write(read.Payload)
			}
			output := received.String()
			if calls.Load() != 1 || strings.Count(output, "\n") != 4 || !strings.Contains(output, `"keyNames":["key"]`) || !strings.Contains(output, `"k8s.io/initial-events-end":"true"`) {
				t.Fatal("watch lost projected events or reconnected")
			}
			for _, secret := range []string{"cHJpdmF0ZS12YWx1ZQ==", "private-snapshot", "must-not-escape", `"namespace":"private"`} {
				if strings.Contains(output, secret) {
					t.Fatal("private event data escaped")
				}
			}
			origin := managedkubernetes.StreamReceiptOrigin{SnapshotDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), ChildInputDigest: strings.Repeat("c", 64), EffectRef: "effect_synthetic", EffectGeneration: 1, StreamRef: "stream_synthetic", StreamGeneration: 1, Operation: operation, TargetRef: credential.target.ID, TargetRevision: int64(credential.target.Revision)}
			receipt, err := stream.HandshakeReceipt(origin)
			if err != nil || receipt.Protocol != managedkubernetes.HTTPWatchV1 {
				t.Fatal("watch lost exact receipt")
			}
			if stream.WriteFrame(t.Context(), value.KubernetesStreamFrame{Channel: "STDIN"}) == nil {
				t.Fatal("watch accepted INPUT")
			}
		})
	}
}

// TestNativeWatchHTTPRejectsMalformedAndOversizedEvents exercises actual bounded decoder before output, without raw fallback or reconnect.
// TestNativeWatchHTTPRejectsMalformedAndOversizedEvents проверяет actual bounded decoder до output без raw fallback либо reconnect.
func TestNativeWatchHTTPRejectsMalformedAndOversizedEvents(t *testing.T) {
	for _, scenario := range []string{"duplicate", "unknown-event", "error", "foreign-kind", "foreign-namespace", "truncated", "oversized", "raw-budget"} {
		t.Run(scenario, func(t *testing.T) {
			registered := watchFixtureSchema(t, "ConfigMap")
			object := watchObjectFixture("ConfigMap", "example")
			typeName := "ADDED"
			switch scenario {
			case "unknown-event":
				typeName = "CUSTOM"
			case "error":
				typeName = "ERROR"
			case "foreign-kind":
				object["kind"] = "Secret"
			case "foreign-namespace":
				object["metadata"].(map[string]any)["namespace"] = "other"
			case "oversized":
				object["data"].(map[string]any)["large"] = strings.Repeat("x", resourceResponseBytes+1)
			}
			raw, _ := json.Marshal(map[string]any{"type": typeName, "object": object})
			if scenario == "duplicate" {
				raw = append([]byte(`{"type":"MODIFIED",`), raw[1:]...)
			}
			if scenario == "truncated" {
				raw = raw[:len(raw)-2]
			}
			credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(raw)
			}))
			input, address, operation := watchInputFixture("configmaps", false)
			maximum := uint64(4 << 20)
			if scenario == "raw-budget" {
				maximum = 64
			}
			now := time.Now()
			stream, err := credential.openWatchHTTP(t.Context(), t.Context(), operation, input, address, registered, nil, now.Add(3*time.Second), now.Add(time.Second), maximum)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			read, err := stream.ReadStream()
			if err == nil || read.CleanEOF || len(read.Payload) != 0 || calls.Load() != 1 {
				t.Fatal("unsafe watch event escaped or reconnected")
			}
		})
	}
}

// TestNativeWatchHTTPChunksAndCancellation preserves complete projected bytes and joins a blocked actual HTTP read after local cancellation.
// TestNativeWatchHTTPChunksAndCancellation сохраняет полные projected bytes и присоединяет blocked actual HTTP read после local cancellation.
func TestNativeWatchHTTPChunksAndCancellation(t *testing.T) {
	for _, cancelRead := range []bool{false, true} {
		registered := watchFixtureSchema(t, "ConfigMap")
		peerDone := make(chan struct{})
		credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer close(peerDone)
			w.Header().Set("Content-Type", "application/json")
			if cancelRead {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			object := watchObjectFixture("ConfigMap", "example")
			object["data"].(map[string]any)["large"] = strings.Repeat("x", 40000)
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "ADDED", "object": object})
		}))
		input, address, operation := watchInputFixture("configmaps", false)
		now := time.Now()
		ctx, cancel := context.WithCancel(t.Context())
		stream, err := credential.openWatchHTTP(ctx, t.Context(), operation, input, address, registered, nil, now.Add(3*time.Second), now.Add(time.Second), 128<<10)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if cancelRead {
			done := make(chan error, 1)
			go func() { _, err := stream.ReadStream(); done <- err }()
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancel became clean EOF")
				}
			case <-time.After(time.Second):
				t.Fatal("watch reader did not join")
			}
		} else {
			var result bytes.Buffer
			frames := 0
			for {
				read, err := stream.ReadStream()
				if err != nil {
					t.Fatal(err)
				}
				if read.CleanEOF {
					break
				}
				frames++
				result.Write(read.Payload)
			}
			if frames != 2 || !json.Valid(bytes.TrimSpace(result.Bytes())) {
				t.Fatal("event was truncated across frames")
			}
			cancel()
		}
		_ = stream.Close()
		select {
		case <-peerDone:
		case <-time.After(time.Second):
			t.Fatal("HTTP peer did not join")
		}
	}
}

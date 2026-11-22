package kubernetestarget

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// resourceWireFixture retains only synthetic counters and a deterministic private API response.
// resourceWireFixture хранит лишь synthetic counters и deterministic private API response.
type resourceWireFixture struct {
	effects         atomic.Int64
	reads           atomic.Int64
	deny            atomic.Bool
	changeSchema    atomic.Bool
	response        string
	status          int
	wire            func(*testing.T, *http.Request, []byte)
	scale           bool
	secret          bool
	deepCRD         bool
	commandAddress  *ResourceAddress
	commandObject   map[string]any
	createOnly      bool
	podBody         string
	streamHandler   http.HandlerFunc
	streamCalls     atomic.Int64
	parentEscalates atomic.Bool
	denySubject     atomic.Bool
	versionBody     string
	versionStatus   int
	versionReads    atomic.Int64
}

// resourceTLSFixture exercises production discovery, selected-identity reviews and HTTPS I/O.
// The synthetic API is deliberately not an apiserver or an owner admission implementation.
// resourceTLSFixture проверяет production discovery, selected-identity reviews и HTTPS I/O.
// Synthetic API явно не является apiserver либо реализацией owner admission.
func resourceTLSFixture(t *testing.T, state *resourceWireFixture) Credential {
	t.Helper()
	document := resourceSchemaFixture(t)
	models := document["components"].(map[string]any)["schemas"].(map[string]any)
	meta := models["Meta"].(map[string]any)["properties"].(map[string]any)
	meta["generateName"] = map[string]any{"type": "string"}
	meta["annotations"] = map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
	meta["managedFields"] = map[string]any{"type": "array", "items": map[string]any{"type": "object", "x-kubernetes-preserve-unknown-fields": true}}
	models["ConfigMap"].(map[string]any)["properties"].(map[string]any)["status"] = map[string]any{"type": "object", "properties": map[string]any{"ready": map[string]any{"type": "boolean"}}}
	if state.scale {
		for kind, version := range map[string]string{"Scale": "autoscaling/v1", "Deployment": "apps/v1"} {
			parts := strings.Split(version, "/")
			models[kind] = map[string]any{"type": "object", "required": []string{"apiVersion", "kind", "metadata", "spec"},
				"x-kubernetes-group-version-kind": []any{map[string]any{"group": parts[0], "version": parts[1], "kind": kind}},
				"properties": map[string]any{"apiVersion": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string"},
					"metadata": map[string]any{"$ref": "#/components/schemas/Meta"}, "spec": map[string]any{"type": "object", "additionalProperties": true}}}
		}
	}
	if state.secret {
		models["Secret"] = map[string]any{"type": "object", "required": []string{"apiVersion", "kind", "metadata"},
			"x-kubernetes-group-version-kind": []any{map[string]any{"group": "", "version": "v1", "kind": "Secret"}},
			"properties":                      map[string]any{"apiVersion": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string"}, "metadata": map[string]any{"$ref": "#/components/schemas/Meta"}, "type": map[string]any{"type": "string"}, "data": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}, "stringData": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}}}
	}
	if state.deepCRD {
		models["Widget"] = map[string]any{"type": "object", "required": []string{"apiVersion", "kind", "metadata", "spec"}, "x-kubernetes-group-version-kind": []any{map[string]any{"group": "data.example", "version": "v1", "kind": "Widget"}}, "properties": map[string]any{"apiVersion": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string"}, "metadata": map[string]any{"$ref": "#/components/schemas/Meta"}, "spec": map[string]any{"type": "object", "x-kubernetes-preserve-unknown-fields": true}}}
	}
	if state.podBody != "" {
		meta["deletionTimestamp"] = map[string]any{"type": "string"}
		models["Pod"] = map[string]any{"type": "object", "required": []string{"apiVersion", "kind", "metadata", "spec"},
			"x-kubernetes-group-version-kind": []any{map[string]any{"group": "", "version": "v1", "kind": "Pod"}},
			"properties": map[string]any{"apiVersion": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string"},
				"metadata": map[string]any{"$ref": "#/components/schemas/Meta"}, "spec": map[string]any{"type": "object", "x-kubernetes-preserve-unknown-fields": true}}}
	}
	commandDiscovery, commandPath := "", ""
	if state.commandAddress != nil {
		address := *state.commandAddress
		commandDiscovery = "/api/" + address.Version
		if address.Group != "" {
			commandDiscovery = "/apis/" + address.Group + "/" + address.Version
		}
		commandPath = commandDiscovery
		if address.Namespaced {
			commandPath += "/namespaces/" + address.Namespace
		}
		commandPath += "/" + address.Resource
		if address.Name != "" {
			commandPath += "/" + address.Name
		}
		kind := state.commandObject["kind"].(string)
		models[kind] = map[string]any{"type": "object", "required": []string{"apiVersion", "kind", "metadata"}, "x-kubernetes-group-version-kind": []any{map[string]any{"group": address.Group, "version": address.Version, "kind": kind}}, "properties": map[string]any{"apiVersion": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string"}, "metadata": map[string]any{"$ref": "#/components/schemas/Meta"}, "spec": map[string]any{"type": "object", "x-kubernetes-preserve-unknown-fields": true}, "subjects": map[string]any{"type": "array", "items": map[string]any{"type": "object", "x-kubernetes-preserve-unknown-fields": true}}, "roleRef": map[string]any{"type": "object", "x-kubernetes-preserve-unknown-fields": true}}}
	}
	rawSchema, _ := json.Marshal(document)
	digest := sha512.Sum512(rawSchema)
	hash := strings.ToUpper(hex.EncodeToString(digest[:]))
	credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if state.commandAddress != nil {
			address := *state.commandAddress
			switch request.URL.Path {
			case commandDiscovery:
				version := address.Version
				if address.Group != "" {
					version = address.Group + "/" + address.Version
				}
				verbs := []string{"get", "patch"}
				if state.createOnly {
					verbs = []string{"create"}
				}
				_ = json.NewEncoder(writer).Encode(map[string]any{"apiVersion": "v1", "kind": "APIResourceList", "groupVersion": version, "resources": []any{map[string]any{"name": address.Resource, "singularName": "", "namespaced": address.Namespaced, "kind": state.commandObject["kind"], "verbs": verbs}}})
				return
			case "/openapi/v3":
				selected := hash
				if state.changeSchema.Load() {
					selected = strings.Repeat("A", 128)
				}
				_ = json.NewEncoder(writer).Encode(map[string]any{"paths": map[string]any{strings.TrimPrefix(commandDiscovery, "/"): map[string]any{"serverRelativeURL": "/openapi/v3" + commandDiscovery + "?hash=" + selected}}})
				return
			case "/openapi/v3" + commandDiscovery:
				_, _ = writer.Write(rawSchema)
				return
			case commandPath:
				if request.Method == http.MethodGet {
					state.reads.Add(1)
					_ = json.NewEncoder(writer).Encode(state.commandObject)
					return
				}
			}
		}
		switch request.URL.Path {
		case "/version":
			state.versionReads.Add(1)
			if state.versionStatus != 0 {
				writer.WriteHeader(state.versionStatus)
			}
			_, _ = io.WriteString(writer, state.versionBody)
		case "/api/v1/namespaces/kube-system":
			_, _ = io.WriteString(writer, `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"kube-system","uid":"629c395b-32aa-425b-814d-5cd9a9a42da6","resourceVersion":"1"},"status":{"phase":"Active"}}`)
		case "/apis/authentication.k8s.io/v1/selfsubjectreviews":
			if state.denySubject.Load() {
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(writer).Encode(authenticationv1.SelfSubjectReview{TypeMeta: metav1.TypeMeta{Kind: "SelfSubjectReview", APIVersion: "authentication.k8s.io/v1"}, Status: authenticationv1.SelfSubjectReviewStatus{UserInfo: subjectFixture()}})
		case "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
			var review authorizationv1.SelfSubjectAccessReview
			if json.NewDecoder(request.Body).Decode(&review) != nil {
				t.Error("invalid selected-identity access review")
			}
			review.Status.Allowed, review.Status.Denied = !state.deny.Load(), state.deny.Load()
			if state.createOnly && review.Spec.ResourceAttributes != nil && review.Spec.ResourceAttributes.Resource == state.commandAddress.Resource && review.Spec.ResourceAttributes.Verb != "create" {
				review.Status.Allowed, review.Status.Denied = false, true
			}
			_ = json.NewEncoder(writer).Encode(review)
		case "/api/v1":
			if state.podBody != "" {
				_, _ = io.WriteString(writer, `{"apiVersion":"v1","kind":"APIResourceList","groupVersion":"v1","resources":[{"name":"pods","singularName":"pod","namespaced":true,"kind":"Pod","verbs":["get"]}]}`)
				return
			}
			if state.secret {
				_, _ = io.WriteString(writer, `{"apiVersion":"v1","kind":"APIResourceList","groupVersion":"v1","resources":[{"name":"secrets","singularName":"secret","namespaced":true,"kind":"Secret","verbs":["get","list","watch","create","update","patch","delete"]}]}`)
				return
			}
			_, _ = io.WriteString(writer, `{"apiVersion":"v1","kind":"APIResourceList","groupVersion":"v1","resources":[{"name":"configmaps","singularName":"configmap","namespaced":true,"kind":"ConfigMap","verbs":["get","list","watch","create","update","patch","delete"]},{"name":"configmaps/status","singularName":"","namespaced":true,"kind":"ConfigMap","verbs":["get","update","patch"]}]}`)
		case "/apis/apps/v1":
			_, _ = io.WriteString(writer, `{"apiVersion":"v1","kind":"APIResourceList","groupVersion":"apps/v1","resources":[{"name":"deployments","singularName":"deployment","namespaced":true,"kind":"Deployment","verbs":["get","list","update","patch","delete"]},{"name":"deployments/scale","singularName":"","namespaced":true,"kind":"Scale","group":"autoscaling","version":"v1","verbs":["get","update","patch"]}]}`)
		case "/apis/data.example/v1":
			_, _ = io.WriteString(writer, `{"apiVersion":"v1","kind":"APIResourceList","groupVersion":"data.example/v1","resources":[{"name":"widgets","singularName":"widget","namespaced":true,"kind":"Widget","verbs":["get","list"]}]}`)
		case "/openapi/v3":
			selected := hash
			if state.changeSchema.Load() {
				selected = strings.Repeat("A", 128)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"paths": map[string]any{"api/v1": map[string]any{"serverRelativeURL": "/openapi/v3/api/v1?hash=" + selected}, "apis/apps/v1": map[string]any{"serverRelativeURL": "/openapi/v3/apis/apps/v1?hash=" + selected}, "apis/data.example/v1": map[string]any{"serverRelativeURL": "/openapi/v3/apis/data.example/v1?hash=" + selected}}})
		case "/openapi/v3/api/v1", "/openapi/v3/apis/apps/v1", "/openapi/v3/apis/data.example/v1":
			_, _ = writer.Write(rawSchema)
		default:
			if state.streamHandler != nil && ((request.URL.Query().Get("watch") == "true" && (request.URL.Path == "/api/v1/namespaces/example/configmaps" || request.URL.Path == "/api/v1/configmaps")) || strings.HasPrefix(request.URL.Path, "/exec/") || strings.HasPrefix(request.URL.Path, "/attach/") || strings.HasPrefix(request.URL.Path, "/portForward/") || request.URL.Path == "/api/v1/namespaces/example/pods/selected/log") {
				state.streamCalls.Add(1)
				state.streamHandler(writer, request)
				return
			}
			if request.Method == http.MethodGet {
				state.reads.Add(1)
				if state.podBody != "" {
					if request.URL.Path != "/api/v1/namespaces/example/pods/selected" {
						t.Error("Pod observation attempted an additional resource GET")
						writer.WriteHeader(http.StatusForbidden)
						return
					}
					_, _ = io.WriteString(writer, state.podBody)
					return
				}
				if state.deepCRD {
					metadata := map[string]any{"name": "selected", "namespace": "example", "uid": "11111111-1111-4111-8111-111111111111", "resourceVersion": "10"}
					_ = json.NewEncoder(writer).Encode(map[string]any{"apiVersion": "data.example/v1", "kind": "Widget", "metadata": metadata, "spec": resourceDeepProjectionFixture()})
					return
				}
				if state.secret {
					_, _ = io.WriteString(writer, resourceSecretWireObject("10"))
					return
				}
				if state.scale {
					if strings.HasSuffix(request.URL.Path, "/scale") {
						_, _ = io.WriteString(writer, resourceScaleWireObject("10"))
					} else {
						_, object, _ := resourcePolicyFixture(t)
						metadata := object["metadata"].(map[string]any)
						metadata["uid"], metadata["resourceVersion"] = "11111111-1111-4111-8111-111111111111", "10"
						if state.parentEscalates.Load() {
							object["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["hostPID"] = true
						}
						_ = json.NewEncoder(writer).Encode(object)
					}
					return
				}
				if strings.HasSuffix(request.URL.Path, "/configmaps") {
					_, _ = io.WriteString(writer, `{"apiVersion":"v1","kind":"ConfigMapList","metadata":{"resourceVersion":"10","continue":"next"},"items":[`+resourceWireObject("10")+`]}`)
				} else {
					_, _ = io.WriteString(writer, resourceWireObject("10"))
				}
				return
			}
			state.effects.Add(1)
			raw, err := io.ReadAll(request.Body)
			if err != nil || request.ProtoMajor != 1 {
				t.Error("effect did not use one complete nonreplayable HTTP/1 body")
			}
			if state.wire != nil {
				state.wire(t, request, raw)
			}
			status := state.status
			if status == 0 {
				status = http.StatusOK
				if request.Method == http.MethodPost {
					status = http.StatusCreated
				}
			}
			writer.WriteHeader(status)
			response := state.response
			if response == "" {
				response = resourceWireObject("11")
				if state.secret {
					response = resourceSecretWireObject("11")
				}
				if state.scale {
					response = resourceScaleWireObject("11")
				}
				if request.Method == http.MethodDelete {
					response = `{"apiVersion":"v1","kind":"Status","status":"Success","details":{"name":"selected","uid":"11111111-1111-4111-8111-111111111111","kind":"configmaps"}}`
				}
			}
			_, _ = io.WriteString(writer, response)
		}
	}))
	return credential
}

// resourceSecretWireObject carries only synthetic private values inside the disposable TLS API.
// resourceSecretWireObject несёт только synthetic private values внутри disposable TLS API.
func resourceSecretWireObject(revision string) string {
	return `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"selected","namespace":"example","uid":"11111111-1111-4111-8111-111111111111","resourceVersion":"` + revision + `","annotations":{"private":"synthetic-private"}},"type":"Opaque","data":{"private":"c3ludGhldGljLXByaXZhdGU="}}`
}

// TestResourceProtectedSecretMaterialIsSeparateAndConsumed verifies actual TLS private writes,
// immediate exclusive binding ownership, failed-binding quarantine and metadata-only results.
// TestResourceProtectedSecretMaterialIsSeparateAndConsumed проверяет actual TLS private writes,
// immediate exclusive binding ownership, failed-binding quarantine и metadata-only results.
func TestResourceProtectedSecretMaterialIsSeparateAndConsumed(t *testing.T) {
	for _, variant := range []string{"create", "patch", "wrong ref", "wrong hash", "private escape"} {
		t.Run(variant, func(t *testing.T) {
			state := &resourceWireFixture{secret: true}
			credential := resourceTLSFixture(t, state)
			operation := "kubernetes.create"
			input := managedkubernetes.OperationInput{Version: "v1", Resource: "secrets", Namespace: "example", Body: json.RawMessage(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"selected","namespace":"example"},"type":"Opaque"}`), Options: json.RawMessage(`{"secretMaterialRef":"krs_owner_material"}`)}
			address := ResourceAddress{Version: "v1", Resource: "secrets", Namespace: "example", Namespaced: true}
			owned := []byte(`{"stringData":{"private":"synthetic-private"}}`)
			if variant == "patch" || variant == "private escape" {
				operation = "kubernetes.patch"
				input.Name, input.UID, input.ResourceVersion = "selected", "11111111-1111-4111-8111-111111111111", "10"
				address.Name, address.UID, address.ResourceVersion = input.Name, input.UID, input.ResourceVersion
				input.Body, input.Options = json.RawMessage(`[]`), json.RawMessage(`{"patchType":"JSON","secretMaterialRef":"krs_owner_material"}`)
				owned = []byte(`[{"op":"copy","from":"/data/private","path":"/data/renamed"}]`)
				if variant == "private escape" {
					owned = []byte(`[{"op":"copy","from":"/data/private","path":"/metadata/annotations/leak"}]`)
				}
			}
			prepared, err := credential.PrepareResourceOperation(context.Background(), operation, input, address, subjectPin(t), nil)
			if err != nil {
				t.Fatal("prepare protected Secret fixture")
			}
			defer prepared.Close()
			if prepared.Observation().State != "" {
				t.Fatal("pending material produced a ready observation")
			}
			if result, err := prepared.Execute(context.Background(), time.Now().Add(5*time.Second)); err == nil || result.Attempted || state.effects.Load() != 0 {
				t.Fatal("pending material reached the API")
			}
			digest := sha256.Sum256(owned)
			hash, ref := hex.EncodeToString(digest[:]), "krs_owner_material"
			if variant == "wrong ref" {
				ref = "krs_foreign_material"
			}
			if variant == "wrong hash" {
				hash = strings.Repeat("a", 64)
			}
			err = prepared.BindSecretMaterial(ref, owned, hash)
			if variant == "wrong ref" || variant == "wrong hash" || variant == "private escape" {
				if err == nil || prepared.Observation().State != "" || strings.Trim(string(owned), "\x00") != "" {
					t.Fatal("failed material binding retained ownership or became ready")
				}
				second := []byte(`{"stringData":{"private":"synthetic-private"}}`)
				if prepared.BindSecretMaterial("krs_owner_material", second, hash) == nil || strings.Trim(string(second), "\x00") != "" {
					t.Fatal("failed material binding was replaced or retained caller bytes")
				}
				return
			}
			if err != nil || prepared.Observation().Validate() != nil {
				t.Fatal("valid private material failed complete preparation")
			}
			state.wire = func(t *testing.T, request *http.Request, raw []byte) {
				if variant == "create" && !strings.Contains(string(raw), "synthetic-private") || variant == "patch" && !strings.Contains(string(raw), "/data/renamed") {
					t.Error("private material never reached its one admitted wire body")
				}
			}
			result, err := prepared.Execute(context.Background(), time.Now().Add(5*time.Second))
			if err != nil || result.Outcome != "SUCCEEDED" || state.effects.Load() != 1 || strings.Contains(string(result.SafeJSON), "synthetic-private") || strings.Contains(string(result.SafeJSON), "c3ludG") || strings.Contains(string(result.SafeJSON), "annotations") {
				t.Fatal("private Secret effect failed or leaked material in its ACK")
			}
			prepared.Close()
			if strings.Trim(string(owned), "\x00") != "" {
				t.Fatal("Close did not clear its exclusively owned material")
			}
		})
	}
}

// resourceScaleWireObject represents the registered autoscaling GVK behind an apps parent.
// resourceScaleWireObject представляет registered autoscaling GVK за apps parent.
func resourceScaleWireObject(revision string) string {
	return `{"apiVersion":"autoscaling/v1","kind":"Scale","metadata":{"name":"selected","namespace":"example","uid":"11111111-1111-4111-8111-111111111111","resourceVersion":"` + revision + `"},"spec":{"replicas":3}}`
}

// TestResourceHTTPSScaleChecksWholeParent proves Scale metadata alone cannot admit an unsafe
// workload and that the admitted mutation uses the observed parent's same UID/RV.
// TestResourceHTTPSScaleChecksWholeParent доказывает, что Scale metadata не допускают unsafe
// workload сами; admitted mutation использует тот же observed parent UID/RV.
func TestResourceHTTPSScaleChecksWholeParent(t *testing.T) {
	for _, unsafe := range []bool{false, true} {
		state := &resourceWireFixture{scale: true}
		state.parentEscalates.Store(unsafe)
		credential := resourceTLSFixture(t, state)
		input := managedkubernetes.OperationInput{Group: "apps", Version: "v1", Resource: "deployments", Namespace: "example", Name: "selected", UID: "11111111-1111-4111-8111-111111111111", ResourceVersion: "10", Subresource: "scale", Options: json.RawMessage(`{"replicas":7,"currentReplicas":3}`)}
		address := ResourceAddress{Group: input.Group, Version: input.Version, Resource: input.Resource, Namespace: input.Namespace, Name: input.Name, UID: input.UID, ResourceVersion: input.ResourceVersion, Subresource: "scale", Namespaced: true}
		prepared, err := credential.PrepareResourceOperation(context.Background(), "kubernetes.scale", input, address, subjectPin(t), nil)
		if unsafe {
			if err == nil || state.effects.Load() != 0 {
				t.Fatal("Scale admitted an escalating parent")
			}
			continue
		}
		if err != nil {
			t.Fatalf("qualified Scale preparation failed: %v", err)
		}
		result, err := prepared.Execute(context.Background(), time.Now().Add(5*time.Second))
		prepared.Close()
		if err != nil || result.Outcome != "SUCCEEDED" || state.effects.Load() != 1 || state.reads.Load() != 2 {
			t.Fatal("Scale did not use exact full-parent preparation and one conditional request")
		}
	}
}

// resourceWireObject returns synthetic business content that mutation ACKs must not echo.
// resourceWireObject возвращает synthetic business content, не выдаваемый mutation ACK.
func resourceWireObject(revision string) string {
	return `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"selected","namespace":"example","uid":"11111111-1111-4111-8111-111111111111","resourceVersion":"` + revision + `","labels":{"existing":"value"}},"data":{"private-business":"not-an-ack"},"status":{"ready":true}}`
}

// TestResourceTLSFixtureProtocol verifies that the synthetic API speaks the actual prerequisite
// protocol before an operation scenario uses it; it issues no owner permit or mutation.
// TestResourceTLSFixtureProtocol проверяет actual prerequisite protocol synthetic API до его
// использования operation scenario; owner permit либо mutation не создаёт.
func TestResourceTLSFixtureProtocol(t *testing.T) {
	credential := resourceTLSFixture(t, &resourceWireFixture{})
	if err := credential.VerifyClusterIdentity(context.Background()); err != nil {
		t.Fatalf("cluster protocol: %v", err)
	}
	address := ResourceAddress{Version: "v1", Resource: "configmaps", Namespaced: true, Namespace: "example", Name: "selected"}
	operation, _ := integrationpackage.KubernetesOperationByKey("kubernetes.get")
	registered, err := credential.discoverResource(context.Background(), subjectPin(t), address, operation)
	if err != nil {
		t.Fatalf("discovery protocol: %v", err)
	}
	prepared := &PreparedResourceOperation{credential: credential, address: address, operation: operation, schema: registered, subjectDigest: subjectPin(t)}
	if err := prepared.prepareObservation(context.Background()); err != nil {
		t.Fatalf("observation protocol: %v", err)
	}
	input := managedkubernetes.OperationInput{Version: "v1", Resource: "configmaps", Namespace: "example", Name: "selected"}
	raw, _ := managedkubernetes.EncodeOperationInput("kubernetes.get", input)
	decoded, _, decodeErr := managedkubernetes.DecodeOperationInput("kubernetes.get", raw)
	if decodeErr != nil || !resourceInputAddressMatches(decoded, address) || validateTarget(credential.target) != nil {
		t.Fatalf("local prerequisite protocol: decode=%v match=%t target=%v", decodeErr, resourceInputAddressMatches(decoded, address), validateTarget(credential.target))
	}
	prepared.ctx, prepared.input = context.Background(), decoded
	if err := prepared.buildResourceRequest(nil); err != nil {
		t.Fatalf("request construction protocol: %v", err)
	}
}

// TestResourceHTTPSPrepareBeginExecuteMatrix covers actual TLS discovery/current reads, native
// mutation wire forms, conditional UID/RV, result minimization and no second execution.
// TestResourceHTTPSPrepareBeginExecuteMatrix проверяет actual TLS discovery/current reads,
// native mutation wire forms, conditional UID/RV, result minimization и no second execution.
func TestResourceHTTPSPrepareBeginExecuteMatrix(t *testing.T) {
	for _, operation := range []string{"create", "get", "list", "list.all-namespaces", "events", "replace", "edit", "apply", "patch", "label", "annotate", "delete", "status.get", "status.update", "status.patch"} {
		t.Run(operation, func(t *testing.T) {
			state := &resourceWireFixture{}
			credential := resourceTLSFixture(t, state)
			input := managedkubernetes.OperationInput{Version: "v1", Resource: "configmaps", Namespace: "example", Name: "selected"}
			address := ResourceAddress{Version: "v1", Resource: "configmaps", Namespaced: true, Namespace: "example", Name: "selected"}
			read := operation == "get" || operation == "status.get" || operation == "list" || operation == "list.all-namespaces" || operation == "events"
			if operation == "list" || operation == "list.all-namespaces" || operation == "events" || operation == "create" {
				input.Name, address.Name = "", ""
			}
			if operation == "list.all-namespaces" {
				input.Namespace, address.Namespace, address.AllNamespaces = "", "", true
			}
			if strings.HasPrefix(operation, "status.") {
				input.Subresource, address.Subresource = "status", "status"
			}
			if !read && operation != "create" {
				input.UID, input.ResourceVersion = "11111111-1111-4111-8111-111111111111", "10"
				address.UID, address.ResourceVersion = input.UID, input.ResourceVersion
			}
			switch operation {
			case "create":
				input.Body = json.RawMessage(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"selected","namespace":"example"},"data":{"safe":"proposed"}}`)
			case "replace", "edit", "apply":
				input.Body = json.RawMessage(resourceWireObject("10"))
				if operation == "apply" {
					input.Options = json.RawMessage(`{"fieldManager":"kodex-test","forceConflicts":true}`)
				}
			case "patch":
				input.Body, input.Options = json.RawMessage(`[{"op":"add","path":"/data/changed","value":"safe"}]`), json.RawMessage(`{"patchType":"JSON"}`)
			case "label", "annotate":
				input.Options = json.RawMessage(`{"assignments":{"new":"value"},"overwrite":false}`)
			case "delete":
				input.Body = json.RawMessage(`{"apiVersion":"v1","kind":"DeleteOptions","propagationPolicy":"Foreground"}`)
			case "status.update":
				input.Body = json.RawMessage(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"selected","namespace":"example","uid":"11111111-1111-4111-8111-111111111111","resourceVersion":"10"},"status":{"ready":false}}`)
			case "status.patch":
				input.Body, input.Options = json.RawMessage(`{"status":{"ready":false}}`), json.RawMessage(`{"patchType":"MERGE"}`)
			}
			state.wire = func(t *testing.T, request *http.Request, raw []byte) {
				if operation == "create" {
					if request.Method != "POST" || !strings.HasSuffix(request.URL.Path, "/configmaps") {
						t.Error("CREATE was not a single collection POST")
					}
				} else if !strings.Contains(string(raw), "11111111-1111-4111-8111-111111111111") || !strings.Contains(string(raw), "10") {
					t.Error("existing effect omitted observed UID/RV preconditions")
				}
				if operation == "apply" && (request.Header.Get("Content-Type") != "application/apply-patch+yaml" || request.URL.Query().Get("force") != "true" || request.URL.Query().Get("fieldManager") != "kodex-test") {
					t.Error("SSA semantics were silently replaced")
				}
			}
			prepared, err := credential.PrepareResourceOperation(context.Background(), "kubernetes."+operation, input, address, subjectPin(t), []string{"private"})
			if err != nil {
				t.Fatalf("production resource preparation failed for %s: %v", operation, err)
			}
			defer prepared.Close()
			if state.effects.Load() != 0 || prepared.Observation().Validate() != nil {
				t.Fatal("preparation performed an effect or returned an invalid owner proposal")
			}
			if operation == "create" && state.reads.Load() != 0 {
				t.Fatal("CREATE added a mandatory object/namespace GET")
			}
			result, err := prepared.Execute(context.Background(), time.Now().Add(5*time.Second))
			if err != nil || result.Outcome != "SUCCEEDED" || len(result.SafeJSON) == 0 {
				t.Fatalf("one admitted synthetic resource operation failed: %s %v", result.ReasonCode, err)
			}
			if !read && (state.effects.Load() != 1 || strings.Contains(string(result.SafeJSON), "not-an-ack")) {
				t.Fatal("mutation was retried or exposed full resource body as ACK")
			}
			if operation == "delete" && result.ResourceVersion != "" {
				t.Fatal("DELETE Status fabricated a post-effect resourceVersion")
			}
			if _, err := prepared.Execute(context.Background(), time.Now().Add(5*time.Second)); err == nil {
				t.Fatal("consumed resource preparation was replayed")
			}
		})
	}
}

// TestResourceHTTPSFreshDenialAndAmbiguousACK keeps changed permissions/schema before effects
// and malformed/redirect/failed responses after dispatch from becoming success or a retry.
// TestResourceHTTPSFreshDenialAndAmbiguousACK отделяет changed permissions/schema до effect
// от malformed/redirect/failed ACK после dispatch, не превращая их в success либо retry.
func TestResourceHTTPSFreshDenialAndAmbiguousACK(t *testing.T) {
	for _, failure := range []string{"revoke", "schema", "expired", "bad ACK", "redirect", "API conflict", "API unavailable"} {
		t.Run(failure, func(t *testing.T) {
			state := &resourceWireFixture{}
			credential := resourceTLSFixture(t, state)
			input := managedkubernetes.OperationInput{Version: "v1", Resource: "configmaps", Namespace: "example", Name: "selected", UID: "11111111-1111-4111-8111-111111111111", ResourceVersion: "10",
				Body: json.RawMessage(`{"data":{"new":"safe"}}`), Options: json.RawMessage(`{"patchType":"MERGE"}`)}
			address := ResourceAddress{Version: "v1", Resource: "configmaps", Namespaced: true, Namespace: "example", Name: input.Name, UID: input.UID, ResourceVersion: "10"}
			prepared, err := credential.PrepareResourceOperation(context.Background(), "kubernetes.patch", input, address, subjectPin(t), nil)
			if err != nil {
				t.Fatal("prepare negative transport fixture")
			}
			defer prepared.Close()
			deadline := time.Now().Add(5 * time.Second)
			switch failure {
			case "revoke":
				state.deny.Store(true)
			case "schema":
				state.changeSchema.Store(true)
			case "expired":
				deadline = time.Now().Add(-time.Second)
			case "bad ACK":
				state.response = strings.ReplaceAll(resourceWireObject("11"), input.UID, "22222222-2222-4222-8222-222222222222")
			case "redirect":
				state.status = http.StatusTemporaryRedirect
			case "API conflict":
				state.status = http.StatusConflict
			case "API unavailable":
				state.status = http.StatusServiceUnavailable
			}
			result, _ := prepared.Execute(context.Background(), deadline)
			beforeWire := failure == "revoke" || failure == "schema" || failure == "expired"
			if result.Outcome == "SUCCEEDED" || len(result.SafeJSON) != 0 || beforeWire && state.effects.Load() != 0 || !beforeWire && state.effects.Load() != 1 {
				t.Fatal("invalid admission/ACK escaped the one-attempt boundary")
			}
			if !managedkubernetes.ValidReasonCode(result.ReasonCode) {
				t.Fatal("lower result cannot pass the closed owner completion reason registry")
			}
			if !beforeWire && failure != "API conflict" && result.Outcome != "UNKNOWN" {
				t.Fatal("ambiguous mutation response was declared stopped")
			}
		})
	}
}

// TestResourceHTTPSNativePatchVariants keeps each native dialect/content-type and UID/RV
// conditional semantics rather than replacing strategic/merge requests with JSON Patch.
// TestResourceHTTPSNativePatchVariants сохраняет каждый native dialect/content-type и UID/RV
// conditional semantics, не подменяя strategic/merge requests JSON Patch.
func TestResourceHTTPSNativePatchVariants(t *testing.T) {
	for _, dialect := range []string{"JSON", "MERGE", "STRATEGIC"} {
		t.Run(dialect, func(t *testing.T) {
			state := &resourceWireFixture{}
			credential := resourceTLSFixture(t, state)
			input := managedkubernetes.OperationInput{Version: "v1", Resource: "configmaps", Namespace: "example", Name: "selected", UID: "11111111-1111-4111-8111-111111111111", ResourceVersion: "10", Body: json.RawMessage(`{"data":{"new":"safe"}}`), Options: json.RawMessage(`{"patchType":"` + dialect + `"}`)}
			if dialect == "JSON" {
				input.Body = json.RawMessage(`[{"op":"add","path":"/data/new","value":"safe"}]`)
			}
			address := ResourceAddress{Version: input.Version, Resource: input.Resource, Namespace: input.Namespace, Namespaced: true, Name: input.Name, UID: input.UID, ResourceVersion: input.ResourceVersion}
			state.wire = func(t *testing.T, request *http.Request, raw []byte) {
				expected := map[string]string{"JSON": "application/json-patch+json", "MERGE": "application/merge-patch+json", "STRATEGIC": "application/strategic-merge-patch+json"}[dialect]
				if request.Method != "PATCH" || request.Header.Get("Content-Type") != expected || !strings.Contains(string(raw), input.UID) || !strings.Contains(string(raw), "10") {
					t.Error("native patch semantics or CAS changed")
				}
			}
			prepared, err := credential.PrepareResourceOperation(context.Background(), "kubernetes.patch", input, address, subjectPin(t), nil)
			if err != nil {
				t.Fatalf("native patch preparation failed: %v", err)
			}
			defer prepared.Close()
			result, err := prepared.Execute(context.Background(), time.Now().Add(5*time.Second))
			if err != nil || result.Outcome != "SUCCEEDED" || state.effects.Load() != 1 {
				t.Fatal("native conditional patch failed")
			}
		})
	}
}

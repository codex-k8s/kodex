package kubernetestarget

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// TestMetadataHelpReturnsUsableSchemaObjects exercises exact command and family help over the selected TLS identity; encoded blobs and unknown command success are rejected.
// TestMetadataHelpReturnsUsableSchemaObjects проверяет справку exact command и family через выбранную TLS identity; encoded blobs и успешный ответ неизвестной команды запрещены.
func TestMetadataHelpReturnsUsableSchemaObjects(t *testing.T) {
	for _, name := range []string{"kubernetes.scale", "scale", "rollout", "unknown-command"} {
		t.Run(name, func(t *testing.T) {
			wire := &resourceWireFixture{}
			credential := resourceTLSFixture(t, wire)
			raw, err := json.Marshal(map[string]any{"options": map[string]any{"commandName": name}})
			if err != nil {
				t.Fatal(err)
			}
			input, _, err := managedkubernetes.DecodeOperationInput("kubernetes.help", raw)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := credential.PrepareResourceOperation(t.Context(), "kubernetes.help", input, ResourceAddress{}, subjectPin(t), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			result, err := prepared.Execute(t.Context(), time.Now().Add(2*time.Second))
			if name == "unknown-command" {
				if err == nil || len(result.SafeJSON) != 0 || result.Outcome == "SUCCEEDED" {
					t.Fatal("unknown command produced successful help")
				}
				return
			}
			if err != nil || result.Outcome != "SUCCEEDED" {
				t.Fatal("selected help failed", err)
			}
			var view struct {
				Commands []struct {
					Operation string         `json:"operation"`
					Family    string         `json:"family"`
					Schema    map[string]any `json:"inputSchema"`
				} `json:"registeredCommands"`
			}
			if json.Unmarshal(result.SafeJSON, &view) != nil || len(view.Commands) == 0 {
				t.Fatal("help did not return JSON schema objects")
			}
			if name == "rollout" && len(view.Commands) < 2 {
				t.Fatal("family help omitted registered commands")
			}
			for _, entry := range view.Commands {
				properties, ok := entry.Schema["properties"].(map[string]any)
				if (entry.Operation != name && entry.Family != name) || entry.Schema["type"] != "object" || !ok || properties["resource"] == nil {
					t.Fatal("help schema lost command input properties")
				}
			}
			if wire.versionReads.Load() != 0 || wire.reads.Load() != 0 || wire.effects.Load() != 0 {
				t.Fatal("help used resource or version request")
			}
		})
	}
}

// TestMetadataResultReadbackRequiresCurrentPrivateIdentity checks stored-result delivery after subject or permission revocation without replaying the metadata request.
// TestMetadataResultReadbackRequiresCurrentPrivateIdentity проверяет выдачу сохранённого результата после отзыва identity или прав без повторения metadata request.
func TestMetadataResultReadbackRequiresCurrentPrivateIdentity(t *testing.T) {
	for _, operation := range []string{"kubernetes.version", "kubernetes.identity", "kubernetes.help", "kubernetes.config.metadata"} {
		t.Run(operation, func(t *testing.T) {
			wire := &resourceWireFixture{}
			credential := resourceTLSFixture(t, wire)
			reader := &streamMaterialFixture{raw: streamCredentialBytes(t, credential)}
			defer clear(reader.raw)
			locator := identityLocatorFixture(t, reader.raw, credential.target)
			snapshot := value.KubernetesOperationSnapshot{Operation: operation, Identity: locator, Target: credential.target, SubjectDigest: subjectPin(t), ExpiresAt: time.Now().Add(time.Minute)}
			child := targetrepo.OperationChild{Operation: operation}
			factory, err := NewResourcePreparer(reader, locator.Namespace)
			if err != nil {
				t.Fatal(err)
			}
			until, err := factory.AuthorizeKubernetesResourceResult(t.Context(), snapshot, child)
			if err != nil || !until.After(time.Now()) || reader.calls.Load() != 1 {
				t.Fatal("metadata result did not refresh selected private identity", err)
			}
			wire.denySubject.Store(true)
			until, err = factory.AuthorizeKubernetesResourceResult(t.Context(), snapshot, child)
			if err == nil || !until.IsZero() {
				t.Fatal("revoked identity retained historical metadata access")
			}
			wire.denySubject.Store(false)
			if operation == "kubernetes.version" {
				wire.deny.Store(true)
				until, err = factory.AuthorizeKubernetesResourceResult(t.Context(), snapshot, child)
				if err == nil || !until.IsZero() {
					t.Fatal("revoked nonresource RBAC retained historical version access")
				}
				wire.deny.Store(false)
			}
			child.Resource = ResourceAddress{Version: "v1", Resource: "secrets", Namespace: "example", Namespaced: true}
			if _, err = factory.AuthorizeKubernetesResourceResult(t.Context(), snapshot, child); err == nil {
				t.Fatal("resource result borrowed metadata eligibility")
			}
			if wire.versionReads.Load() != 0 || wire.reads.Load() != 0 || wire.effects.Load() != 0 {
				t.Fatal("historical result authorization repeated target read or effect")
			}
		})
	}
}

// TestMetadataOperationsUseSelectedTLSAndClosedProjection checks actual native requests and safe output, without replacing Kubernetes discovery/authentication with an owner mock.
// TestMetadataOperationsUseSelectedTLSAndClosedProjection проверяет реальные native requests и safe output, не заменяя Kubernetes discovery/authentication owner mock.
func TestMetadataOperationsUseSelectedTLSAndClosedProjection(t *testing.T) {
	for _, operation := range []string{"kubernetes.version", "kubernetes.identity", "kubernetes.help", "kubernetes.config.metadata"} {
		t.Run(operation, func(t *testing.T) {
			state := &resourceWireFixture{versionBody: `{"major":"1","minor":"35","gitVersion":"v1.35.5","platform":"linux/amd64","extra":"private-version-sentinel"}`}
			credential := resourceTLSFixture(t, state)
			input, _, err := managedkubernetes.DecodeOperationInput(operation, []byte(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := credential.PrepareResourceOperation(t.Context(), operation, input, ResourceAddress{}, subjectPin(t), nil)
			if err != nil {
				t.Fatal("metadata preparation failed", err)
			}
			defer prepared.Close()
			pin, ready := prepared.Binding()
			if !ready || pin.Observation != (managedkubernetes.ResourceObservation{}) || pin.Operation != operation {
				t.Fatal("metadata fabricated a resource observation")
			}
			if state.versionReads.Load() != 0 {
				t.Fatal("business read happened before Begin boundary")
			}
			result, err := prepared.Execute(t.Context(), time.Now().Add(2*time.Second))
			if err != nil || result.Outcome != "SUCCEEDED" {
				t.Fatal("metadata read failed", err, result.Outcome)
			}
			if state.effects.Load() != 0 || state.reads.Load() != 0 {
				t.Fatal("metadata read used a resource path or mutation")
			}
			var got map[string]any
			if json.Unmarshal(result.SafeJSON, &got) != nil {
				t.Fatal("invalid projection")
			}
			switch operation {
			case "kubernetes.version":
				if state.versionReads.Load() != 1 || got["serverVersion"] == nil {
					t.Fatal("version path missing")
				}
			case "kubernetes.identity":
				if got["username"] != subjectFixture().Username || got["groups"] == nil || got["extra"] != nil {
					t.Fatal("selected subject projection changed")
				}
			case "kubernetes.config.metadata":
				if got["targetRef"] != credential.target.ID || got["clusterUID"] != credential.target.ClusterUID || len(got) != 4 {
					t.Fatal("managed config projection changed")
				}
			case "kubernetes.help":
				if got["registeredCommands"] == nil || got["effectiveReadinessChecked"] != false {
					t.Fatal("help claimed effective readiness")
				}
			}
			for _, sentinel := range []string{"private-version-sentinel", "synthetic.example/identity", credential.token, credential.target.ServerURL} {
				if sentinel != "" && strings.Contains(string(result.SafeJSON), sentinel) {
					t.Fatal("private field entered metadata projection")
				}
			}
			_, err = prepared.Execute(t.Context(), time.Now().Add(time.Second))
			if err == nil || state.versionReads.Load() > 1 {
				t.Fatal("metadata preparation replayed")
			}
		})
	}
}

// TestMetadataOperationsRejectRevocationAndUntrustedResponses prevents credentials, malformed API replies and resource addresses from borrowing the addressless path.
// TestMetadataOperationsRejectRevocationAndUntrustedResponses запрещает отзыв, malformed API replies и resource addresses обходить безадресную boundary.
func TestMetadataOperationsRejectRevocationAndUntrustedResponses(t *testing.T) {
	for _, mode := range []string{"subject", "rbac", "status", "duplicate", "null", "missing", "foreign-address"} {
		t.Run(mode, func(t *testing.T) {
			state := &resourceWireFixture{versionBody: `{"major":"1","minor":"35","gitVersion":"v1.35.5"}`}
			credential := resourceTLSFixture(t, state)
			address := ResourceAddress{}
			if mode == "foreign-address" {
				address = ResourceAddress{Version: "v1", Resource: "secrets", Namespace: "example", Namespaced: true}
			}
			input, _, _ := managedkubernetes.DecodeOperationInput("kubernetes.version", []byte(`{}`))
			prepared, err := credential.PrepareResourceOperation(t.Context(), "kubernetes.version", input, address, subjectPin(t), nil)
			if mode == "foreign-address" {
				if prepared != nil {
					prepared.Close()
				}
				if err == nil {
					t.Fatal("metadata accepted resource coordinates")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			switch mode {
			case "subject":
				state.denySubject.Store(true)
			case "rbac":
				state.deny.Store(true)
			case "status":
				state.versionStatus = 500
			case "duplicate":
				state.versionBody = `{"major":"1","minor":"35","gitVersion":"v1","gitVersion":"v2"}`
			case "null":
				state.versionBody = `{"major":"1","minor":"35","gitVersion":null}`
			case "missing":
				state.versionBody = `{"major":"1","minor":"35"}`
			}
			result, err := prepared.Execute(t.Context(), time.Now().Add(2*time.Second))
			if err == nil || result.Outcome == "SUCCEEDED" || len(result.SafeJSON) != 0 {
				t.Fatal("invalid metadata became successful output")
			}
			if (mode == "subject" || mode == "rbac") && state.versionReads.Load() != 0 {
				t.Fatal("revoked identity read metadata")
			}
			_, _ = prepared.Execute(t.Context(), time.Now().Add(time.Second))
			if state.versionReads.Load() > 1 {
				t.Fatal("failed read replayed")
			}
		})
	}
}

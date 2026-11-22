package kubernetestarget

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// TestResourcePreparerUsesOnlyPinnedPrivateIdentity exercises the actual embedded parser and native TLS preparation.
// TestResourcePreparerUsesOnlyPinnedPrivateIdentity проверяет actual embedded parser и native TLS preparation.
func TestResourcePreparerUsesOnlyPinnedPrivateIdentity(t *testing.T) {
	for _, variant := range []string{"success", "corrupt", "foreign-namespace", "expired"} {
		t.Run(variant, func(t *testing.T) {
			wire := &resourceWireFixture{}
			credential := resourceTLSFixture(t, wire)
			reader := &streamMaterialFixture{raw: streamCredentialBytes(t, credential)}
			defer clear(reader.raw)
			locator := identityLocatorFixture(t, reader.raw, credential.target)
			input := managedkubernetes.OperationInput{Version: "v1", Resource: "configmaps", Namespace: "example", Name: "selected"}
			address := managedkubernetes.ResourceAddress{Version: input.Version, Resource: input.Resource, Namespace: input.Namespace, Name: input.Name, Namespaced: true}
			snapshot := value.KubernetesOperationSnapshot{Operation: "kubernetes.get", Input: input, Resource: &address, Identity: locator, Target: credential.target, SubjectDigest: subjectPin(t), ExpiresAt: time.Now().Add(time.Minute)}
			factory, err := NewResourcePreparer(reader, locator.Namespace)
			if err != nil {
				t.Fatal(err)
			}
			switch variant {
			case "corrupt":
				reader.corrupt.Store(true)
			case "foreign-namespace":
				snapshot.Identity.Namespace = "foreign"
			case "expired":
				snapshot.ExpiresAt = time.Now().Add(-time.Second)
			}
			prepared, err := factory.PrepareKubernetesResource(t.Context(), snapshot)
			if variant != "success" {
				if err == nil || prepared != nil || wire.reads.Load() != 0 || wire.effects.Load() != 0 {
					t.Fatal("invalid private selection reached resource API")
				}
				if (variant == "foreign-namespace" || variant == "expired") && reader.calls.Load() != 0 {
					t.Fatal("foreign selection read private storage")
				}
				return
			}
			if err != nil || prepared == nil || reader.calls.Load() != 1 || wire.effects.Load() != 0 {
				t.Fatal("native preparation did not use exact private identity once")
			}
			defer prepared.Close()
			pin, ready := prepared.Binding()
			if !ready || pin.TargetDigest != snapshot.Target.MetadataDigest() || pin.SubjectDigest != snapshot.SubjectDigest {
				t.Fatal("private factory changed native binding")
			}
			// The storage timeout context is already canceled; the prepared request still owns caller lifetime.
			// Storage timeout context уже отменён; prepared request сохраняет caller lifetime.
			result, err := prepared.Execute(t.Context(), time.Now().Add(5*time.Second))
			if err != nil || result.Outcome != "SUCCEEDED" || wire.effects.Load() != 0 {
				t.Fatal("storage context canceled the prepared native request")
			}
		})
	}
}

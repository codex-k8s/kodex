package integrationegress

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestProjectionPreflightClosed checks both paths stop before writes on invalid document or absent admission.
// TestProjectionPreflightClosed проверяет отказ обоих путей до записи при неверном документе или отсутствии admission.
func TestProjectionPreflightClosed(t *testing.T) {
	for _, which := range []string{"publish", "readback"} {
		t.Run(which, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			publisher, err := New(client)
			if err != nil {
				t.Fatal(err)
			}
			run := func(doc shared.Document) (string, error) {
				if which == "publish" {
					return publisher.PublishWithStage(t.Context(), doc)
				}
				return publisher.CheckWithStage(t.Context(), doc)
			}
			stage, err := run(shared.Document{})
			if stage != "document" || !errors.Is(err, ErrInvalid) || len(client.Actions()) != 0 {
				t.Fatal("invalid document reached client")
			}
			stage, err = run(documentFixture())
			if stage != "admission" || !errors.Is(err, ErrUnavailable) {
				t.Fatal("missing admission did not fail closed")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("preflight performed a mutation")
				}
			}
		})
	}
}

// TestProjectionPreflightSuccess checks exact rendered objects after all three fake admission reads.
// TestProjectionPreflightSuccess проверяет точные объекты после чтения трёх synthetic admission.
func TestProjectionPreflightSuccess(t *testing.T) {
	client := fake.NewSimpleClientset()
	for _, source := range []func() (map[string]any, map[string]any){mailpolicy.PublicationAdmissionResources, shared.PublicationAdmissionResources, shared.CreationBoundaryResources} {
		policySource, bindingSource := source()
		raw, err := json.Marshal(policySource)
		if err != nil {
			t.Fatal(err)
		}
		var policy admissionv1.ValidatingAdmissionPolicy
		if err := json.Unmarshal(raw, &policy); err != nil {
			t.Fatal(err)
		}
		policy.UID, policy.ResourceVersion, policy.Generation = "synthetic-policy", "1", 1
		policy.Status = admissionv1.ValidatingAdmissionPolicyStatus{ObservedGeneration: 1, TypeChecking: &admissionv1.TypeChecking{}}
		raw, err = json.Marshal(bindingSource)
		if err != nil {
			t.Fatal(err)
		}
		var binding admissionv1.ValidatingAdmissionPolicyBinding
		if err := json.Unmarshal(raw, &binding); err != nil {
			t.Fatal(err)
		}
		binding.UID, binding.ResourceVersion = "synthetic-binding", "1"
		if err := client.Tracker().Add(&policy); err != nil {
			t.Fatal(err)
		}
		if err := client.Tracker().Add(&binding); err != nil {
			t.Fatal(err)
		}
	}
	publisher, err := New(client)
	if err != nil {
		t.Fatal(err)
	}
	doc := documentFixture()
	wantConfig, wantNetwork, err := projectionObjects(doc)
	if err != nil {
		t.Fatal(err)
	}
	config, network, stage, err := publisher.prepareProjection(t.Context(), doc)
	if err != nil || stage != "" || !reflect.DeepEqual(config, wantConfig) || !reflect.DeepEqual(network, wantNetwork) {
		t.Fatal("preflight changed rendered objects")
	}
	if len(client.Actions()) != 6 {
		t.Fatal("preflight did not read exactly three admission pairs")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("preflight performed a mutation")
		}
	}
}

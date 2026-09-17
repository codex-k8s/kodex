package admissioncontroller

import (
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestTrustedAdmissionRenderRemovesAuthorityWithoutChangingProtectedProfile(t *testing.T) {
	renderer, err := NewScriptRenderer(filepath.Join(repositoryRoot(), "tools", "render-image-admission-job.sh"))
	if err != nil {
		t.Fatal(err)
	}
	policy := completeTestPolicy()
	policies, err := readAdmissionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	var admissionPolicy admissionPolicyDocument
	for _, candidate := range policies {
		if candidate.Metadata.Name == "kodex-image-admission-controller-jobs" {
			admissionPolicy = candidate
		}
	}
	if admissionPolicy.Metadata.Name == "" {
		t.Fatal("canonical admission policy missing")
	}
	runID := "v20260916120000-" + testOrchestrationRevision
	protected, err := renderer.Render(t.Context(), policy, "staging", runID, "claim")
	if err != nil || len(protected.Job.Spec.Template.Spec.InitContainers) != 3 {
		t.Fatalf("protected render changed: %v", err)
	}
	policy.Labels["kodex.dev/security-profile"] = "trusted-cluster"
	delete(policy.Data, "authorityImage")
	delete(policy.Data, "authorityIssuerImage")
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			rendered, err := renderer.Render(t.Context(), policy, "staging", runID, phase)
			if err != nil {
				t.Fatal(err)
			}
			pod := rendered.Job.Spec.Template
			if pod.Labels["kodex.dev/security-profile"] != "trusted-cluster" || len(pod.Spec.InitContainers) != 0 || len(pod.Spec.Containers) != 1 {
				t.Fatal("trusted profile or container contract differs")
			}
			if rendered.Job.Annotations["kodex.dev/admission-run-sha256"] == protected.Job.Annotations["kodex.dev/admission-run-sha256"] {
				t.Fatal("profiles share an immutable execution identity")
			}
			for _, volume := range pod.Spec.Volumes {
				if strings.Contains(volume.Name, "authority") || strings.Contains(volume.Name, "grant") || volume.Name == "control-plane-ca" {
					t.Fatal("authority volume remains")
				}
			}
			values := map[string]string{}
			for _, entry := range pod.Spec.Containers[0].Env {
				values[entry.Name] = entry.Value
				if strings.Contains(entry.Name, "AUTHORITY") || strings.Contains(entry.Name, "GRANT") || strings.Contains(entry.Name, "TLS") {
					t.Fatal("authority environment remains")
				}
			}
			ownerPhase := phase == "claim" || phase == "admit" || phase == "promote"
			if ownerPhase && (values["KODEX_RPC_PROFILE"] != "trusted-cluster" || values["IMAGE_OWNER_CONTROL_PLANE_TARGET"] != "control-plane.kodex-system.svc:8443" || values["IMAGE_OWNER_STATE_FILE"] != "/work/owner-claim.json") {
				t.Fatal("owner RPC binding is incomplete")
			}
			if !ownerPhase && values["KODEX_RPC_PROFILE"] != "" {
				t.Fatal("non-RPC phase received an RPC profile")
			}
			if err := prepareRendered(rendered, "kodex-system", runID, phase); err != nil {
				t.Fatal(err)
			}
			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rendered.Job)
			if err != nil {
				t.Fatal(err)
			}
			assertPolicyAccepts(t, admissionPolicy, object, policy)
			bad := rendered.Job.DeepCopy()
			bad.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "internal-rpc-authority-issuer"}}
			object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(bad)
			if err != nil {
				t.Fatal(err)
			}
			assertPolicyRejects(t, admissionPolicy, object, policy)
			bad = rendered.Job.DeepCopy()
			delete(bad.Spec.Template.Labels, "kodex.dev/security-profile")
			object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(bad)
			if err != nil {
				t.Fatal(err)
			}
			assertPolicyRejects(t, admissionPolicy, object, policy)
			if ownerPhase {
				bad = rendered.Job.DeepCopy()
				for index := range bad.Spec.Template.Spec.Containers[0].Env {
					entry := &bad.Spec.Template.Spec.Containers[0].Env[index]
					if entry.Name == "IMAGE_OWNER_CONTROL_PLANE_TARGET" {
						entry.Value = "outside.example.test:8443"
					}
				}
				object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(bad)
				if err != nil {
					t.Fatal(err)
				}
				assertPolicyRejects(t, admissionPolicy, object, policy)
			}
		})
	}
	policy.Labels["kodex.dev/security-profile"] = "unknown"
	if _, err := renderer.Render(t.Context(), policy, "staging", runID, "claim"); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

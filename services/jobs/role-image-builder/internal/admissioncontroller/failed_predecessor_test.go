package admissioncontroller

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFailedPredecessorRecoveryRequiresFreshExactJob(t *testing.T) {
	for _, phase := range []string{"scan", "sign"} {
		for _, scenario := range []string{"valid", "foreign_run", "replaced_uid", "wrong_phase", "not_failed"} {
			t.Run(phase+"/"+scenario, func(t *testing.T) {
				runID := "v20260822120000-" + testOrchestrationRevision
				rendered, err := (testRenderer{}).Render(t.Context(), testPolicy(), "production", runID, phase)
				if err != nil || prepareRendered(rendered, testConfig().Namespace, runID, phase) != nil {
					t.Fatal("fixture render failed")
				}
				previous := rendered.Job
				previous.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
				fresh := previous.DeepCopy()
				switch scenario {
				case "foreign_run":
					fresh.Annotations[runIDAnnotation] = "v20260822120001-" + testOrchestrationRevision
				case "replaced_uid":
					fresh.UID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
				case "wrong_phase":
					fresh.Labels[phaseLabel] = "claim"
				case "not_failed":
					fresh.Status.Conditions = nil
				}
				client := fake.NewClientset(fresh)
				controller, err := New(client, testRenderer{}, testConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
				if err != nil {
					t.Fatal(err)
				}
				workspace := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{idLabel: previous.Labels[idLabel]}}}
				err = controller.ensureAdmissionPhase(t.Context(), testPolicy(), workspace, runID, "admit", []batchv1.Job{*previous})
				if scenario != "valid" {
					if err == nil {
						t.Fatal("untrusted failed predecessor accepted")
					}
					jobs, _ := client.BatchV1().Jobs(testConfig().Namespace).List(t.Context(), metav1.ListOptions{})
					if len(jobs.Items) != 1 {
						t.Fatal("rejection created a recovery job")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				job, err := client.BatchV1().Jobs(testConfig().Namespace).Get(t.Context(), "mc-admit-"+previous.Labels[idLabel]+"-admit", metav1.GetOptions{})
				if err != nil || len(job.Spec.Template.Spec.Containers[0].Command) != 4 || job.Spec.Template.Spec.Containers[0].Command[3] != strings.ToUpper(phase)+"_PREDECESSOR_FAILED" || job.Annotations[failedPredecessorUID] != string(previous.UID) {
					t.Fatal("recovery binding missing")
				}
				if err := controller.ensureAdmissionPhase(t.Context(), testPolicy(), workspace, runID, "admit", []batchv1.Job{*previous}); err != nil {
					t.Fatal("exact recovery replay failed", err)
				}
			})
		}
	}
}

func TestFailedPredecessorRecoveryRenderedCEL(t *testing.T) {
	policies, err := readAdmissionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	var policy admissionPolicyDocument
	for _, candidate := range policies {
		if candidate.Metadata.Name == "kodex-image-admission-controller-jobs" {
			policy = candidate
		}
	}
	renderer, err := NewScriptRenderer(filepath.Join(repositoryRoot(), "tools/render-image-admission-job.sh"))
	if err != nil {
		t.Fatal(err)
	}
	owner := completeTestPolicy()
	runID := "v20260822120000-" + testOrchestrationRevision
	for _, phase := range []string{"scan", "admit"} {
		rendered, err := renderer.Render(t.Context(), owner, "production", runID, phase)
		if err != nil || prepareRendered(rendered, "kodex-system", runID, phase) != nil {
			t.Fatal("render failed", err)
		}
		if phase == "scan" {
			for _, volume := range rendered.Job.Spec.Template.Spec.Volumes {
				if volume.Name == "tmp" && volume.EmptyDir.SizeLimit.String() != "32Gi" {
					t.Fatal("full-image scratch remains undersized")
				}
			}
			main := rendered.Job.Spec.Template.Spec.Containers[0]
			if main.Resources.Limits.Memory().String() != "2Gi" {
				t.Fatal("memory changed without OOM evidence")
			}
			if limit, ok := main.Resources.Limits[corev1.ResourceEphemeralStorage]; ok && limit.Value() < 32<<30 {
				t.Fatal("ephemeral limit smaller than scratch")
			}
		} else {
			rendered.Job.Spec.Template.Spec.Containers[0].Command = append(rendered.Job.Spec.Template.Spec.Containers[0].Command, "SCAN_PREDECESSOR_FAILED")
			rendered.Job.Annotations[failedPredecessorUID] = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
		}
		object, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(rendered.Job)
		assertPolicyAccepts(t, policy, object, owner)
		if phase == "admit" {
			for _, mutation := range []string{"unknown_code", "foreign_phase", "missing_uid"} {
				bad := rendered.Job.DeepCopy()
				switch mutation {
				case "unknown_code":
					bad.Spec.Template.Spec.Containers[0].Command[3] = "UNKNOWN"
				case "foreign_phase":
					bad.Labels[phaseLabel] = "scan"
				case "missing_uid":
					delete(bad.Annotations, failedPredecessorUID)
				}
				object, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(bad)
				assertPolicyRejects(t, policy, object, owner)
			}
		}
	}
}

func TestTechnicalFailureDoesNotFabricateRejectedEvidence(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(repositoryRoot(), "deploy/k8s/base/image-supply-chain/image-admission.sh"))
	if err != nil {
		t.Fatal(err)
	}
	function := func(name string) string {
		start := strings.Index(string(source), name+"() {\n")
		if start < 0 {
			t.Fatal("production function absent", name)
		}
		end := strings.Index(string(source)[start:], "\n}\n")
		if end < 0 {
			t.Fatal("production function is incomplete", name)
		}
		return string(source)[start : start+end+3]
	}
	for _, forbidden := range []string{"write_technical_rejection() {", "reject_failed_predecessor() {", "kodex.dev/vulnerability-evidence-unavailable/v1"} {
		if strings.Contains(string(source), forbidden) {
			t.Fatal("legacy fabricated evidence path retained", forbidden)
		}
	}
	for _, phase := range []string{"claim", "scan", "sign", "admit"} {
		for _, rpc := range []string{"available", "unavailable"} {
			t.Run(phase+"/"+rpc, func(t *testing.T) {
				work := t.TempDir()
				original := "{\"immutable\":\"fixture-original-bytes\"}\n"
				for _, name := range []string{"owner-claim.json", "tool-inventory.json", "provenance.json", "sbom.json", "vulnerability.json"} {
					if err := os.WriteFile(filepath.Join(work, name), []byte(original), 0600); err != nil {
						t.Fatal(err)
					}
				}
				program := "set -eu\numask 077\nadmission_phase=" + phase + "\nADMISSION_RUN_ID=fixture-run\n"
				program += `image-admission-bridge() {
  [ "$1" = fail ] && [ "$IMAGE_OWNER_ADMISSION_FAILURE_CODE" = ADMISSION_WORKER_FAILED ] || exit 8
  printf '%s\n' "$1" >> /work/rpc-calls
`
				if rpc == "unavailable" {
					program += "  printf '%s\\n' 'image admission bridge failed: Unavailable' >&2\n  return 1\n"
				} else {
					program += "  return 0\n"
				}
				program += "}\n"
				for _, name := range []string{"classify_owner_failure", "read_owner_failure_code", "persist_owner_failure_code", "write_marker", "record_owner_failure", "fail"} {
					program += function(name) + "\n"
				}
				program += "fail 'technical fixture failure'\n"
				program = strings.ReplaceAll(program, "/work/", work+"/")
				program = strings.ReplaceAll(program, "image-admission-bridge", "fixture_bridge")
				command := exec.CommandContext(t.Context(), "sh", "-c", program)
				output, failure := command.CombinedOutput()
				callbackExpected := phase == "claim" || phase == "admit"
				if callbackExpected && rpc == "unavailable" && !strings.Contains(string(output), "image admission bridge failed: Unavailable") {
					t.Fatal("closed terminal callback diagnostic was discarded")
				}
				if phase == "admit" && rpc == "available" {
					if failure != nil {
						t.Fatalf("owner terminal receipt failed: %v: %s", failure, output)
					}
				} else if failure == nil {
					t.Fatal("failed predecessor unexpectedly succeeded")
				}
				for _, name := range []string{"owner-claim.json", "tool-inventory.json", "provenance.json", "sbom.json", "vulnerability.json"} {
					raw, err := os.ReadFile(filepath.Join(work, name))
					if err != nil || string(raw) != original {
						t.Fatal("technical failure changed original evidence", name)
					}
				}
				for _, name := range []string{"verdict", "signature.complete", "admission.receipt.json", "risk-acceptance.json"} {
					if _, err := os.Stat(filepath.Join(work, name)); !os.IsNotExist(err) {
						t.Fatal("technical failure fabricated evidence", name)
					}
				}
				calls, _ := os.ReadFile(filepath.Join(work, "rpc-calls"))
				wantsRPC := phase == "claim" || phase == "admit"
				if (len(calls) > 0) != wantsRPC {
					t.Fatal("failure used incorrect owner identity")
				}
				_, marker := os.Stat(filepath.Join(work, "admission.failed"))
				if (marker == nil) != (wantsRPC && rpc == "available") {
					t.Fatal("terminal receipt marker does not match actual owner result")
				}
			})
		}
	}
}

func TestFailedPredecessorTerminalCallbackIgnoresIncompleteEvidence(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(repositoryRoot(), "deploy/k8s/base/image-supply-chain/image-admission.sh"))
	if err != nil {
		t.Fatal(err)
	}
	entry := strings.Index(string(source), "\nrequire_policy\n\n")
	if entry < 0 {
		t.Fatal("production admission entrypoint absent")
	}
	for _, predecessor := range []string{"SCAN_PREDECESSOR_FAILED", "SIGN_PREDECESSOR_FAILED", "ADMIT_PREDECESSOR_FAILED"} {
		t.Run(predecessor, func(t *testing.T) {
			work := t.TempDir()
			const retainedClaim = "{\"immutable\":\"fixture-claim\"}\n"
			for name, raw := range map[string]string{"owner-claim.json": retainedClaim, "claim.complete": "fixture-run\n", "vulnerability-report.json": "incomplete scan projection\n"} {
				if err := os.WriteFile(filepath.Join(work, name), []byte(raw), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Выполняется настоящий admit entrypoint. Stub заменяет только policy
			// preflight и внешний RPC; signed record и 26 descriptors не создаются.
			program := string(source[:entry]) + `
ADMISSION_RUN_ID=fixture-run
require_policy() { :; }
fixture_bridge() {
  [ "$1" = fail ] && [ "$IMAGE_OWNER_ADMISSION_FAILURE_CODE" = ADMISSION_WORKER_FAILED ] || exit 8
  if [ ! -f /work/fixture-rpc-available ]; then
    printf '%s\n' 'image admission bridge failed: Unavailable' >&2
    return 1
  fi
}
` + string(source[entry:])
			program = strings.ReplaceAll(program, "/work/", work+"/")
			program = strings.ReplaceAll(program, "image-admission-bridge", "fixture_bridge")
			run := func() ([]byte, error) {
				return exec.CommandContext(t.Context(), "sh", "-c", program, "fixture", "admit", predecessor).CombinedOutput()
			}
			output, err := run()
			if err == nil || !strings.Contains(string(output), "image admission bridge failed: Unavailable") {
				t.Fatal("callback outage lost failure or its closed diagnostic")
			}
			if _, err := os.Stat(filepath.Join(work, "admission.failed")); !os.IsNotExist(err) {
				t.Fatal("callback outage fabricated a terminal receipt marker")
			}
			if err := os.WriteFile(filepath.Join(work, "fixture-rpc-available"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if output, err := run(); err != nil {
				t.Fatalf("exact callback recovery depended on incomplete scan evidence: %v: %s", err, output)
			}
			for name, expected := range map[string]string{"owner-claim.json": retainedClaim, "owner-failure-code": "ADMISSION_WORKER_FAILED\n", "admission.failed": "fixture-run\n", "vulnerability-report.json": "incomplete scan projection\n"} {
				if raw, err := os.ReadFile(filepath.Join(work, name)); err != nil || string(raw) != expected {
					t.Fatal("recovery changed immutable state or lost terminal marker", name)
				}
			}
			for _, name := range []string{"signature.complete", "verdict", "admission.receipt.json", "evidence.oci.manifest.json"} {
				if _, err := os.Stat(filepath.Join(work, name)); !os.IsNotExist(err) {
					t.Fatal("terminal callback fabricated signed record evidence", name)
				}
			}
		})
	}
}

package admissioncontroller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
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

func TestFailedPredecessorScriptRetainsActualEvidenceAndRejectsMissingInventory(t *testing.T) {
	directory := t.TempDir()
	validator := filepath.Join(directory, "image-tool-inventory-validator")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", validator, "./cmd/image-tool-inventory-validator")
	build.Dir = filepath.Join(repositoryRoot(), "services/jobs/role-image-builder")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fixture validator: %v: %s", err, output)
	}
	source, err := os.ReadFile(filepath.Join(repositoryRoot(), "deploy/k8s/base/image-supply-chain/image-admission.sh"))
	if err != nil {
		t.Fatal(err)
	}
	functions := func(name string) string {
		start := strings.Index(string(source), name+"() {\n")
		if start < 0 {
			t.Fatal("production function missing", name)
		}
		end := strings.Index(string(source)[start:], "\n}\n")
		return string(source)[start : start+end+3]
	}
	for _, scenario := range []string{"evicted", "existing_rejection", "stale_marker", "missing_inventory", "foreign_inventory", "corrupt_inventory", "wrong_hash", "unknown_phase"} {
		t.Run(scenario, func(t *testing.T) {
			work := t.TempDir()
			digest := strings.Repeat("a", 64)
			write := func(name string, raw []byte) {
				if err := os.WriteFile(filepath.Join(work, name), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			provenance := `{"buildType":"fixture","builderId":"fixture","immutableBuildSHA256":"` + digest + `","manifestDigest":"sha256:` + digest + `","organizationRef":"org_fixture01","policyRevision":1,"policySHA256":"` + digest + `","projectRef":"","schema":"kodex.dev/image-provenance-binding/v2","scopeKind":"ORGANIZATION","specSHA256":"` + digest + `"}`
			sha := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
			provenanceSHA := sha([]byte(provenance))
			write("provenance.json", []byte(provenance))
			write("provenance.sha256", []byte(provenanceSHA))
			write("owner-claim.json", []byte(`{"artifactId":"imgart_fixture01","manifestDigest":"sha256:`+digest+`","specSHA256":"`+digest+`","immutableBuildSHA256":"`+digest+`","provenanceSHA256":"`+provenanceSHA+`","policyRevision":1,"policySHA256":"`+digest+`"}`))
			manifest := runtimecontract.ImageToolManifest{Schema: runtimecontract.ImageInventorySchema, SpecSHA256: digest, ImmutableBuildSHA256: digest, RuntimeContractSHA256: digest, Platform: "linux/amd64"}
			for _, probe := range runtimecontract.ImageToolProbes() {
				manifest.Tools = append(manifest.Tools, runtimecontract.ImageToolObservation{Name: probe.Name, Status: "MISSING", Required: probe.Required})
			}
			rawManifest, _ := json.Marshal(manifest)
			inventory := runtimecontract.ImageToolInventory{Schema: runtimecontract.ImageInventoryBindingSchema, ImageDigest: "sha256:" + digest, ProvenanceSHA256: provenanceSHA, Platforms: []runtimecontract.ImagePlatformInventory{{PlatformDigest: "sha256:" + digest, ManifestSHA256: runtimecontract.ImageInventorySHA256(rawManifest), Manifest: manifest}}}
			if scenario == "foreign_inventory" {
				inventory.ImageDigest = "sha256:" + strings.Repeat("b", 64)
			}
			raw, _ := json.Marshal(inventory)
			if scenario == "corrupt_inventory" {
				raw = []byte(`{"unknown":true}`)
			}
			if scenario != "missing_inventory" {
				write("tool-inventory.json", raw)
				write("tool-inventory.sha256", []byte(sha(raw)))
			}
			if scenario == "wrong_hash" {
				write("tool-inventory.sha256", []byte(strings.Repeat("b", 64)))
			}
			if scenario == "existing_rejection" || scenario == "stale_marker" {
				write("verdict", []byte("REJECTED"))
				marker := "fixture-run"
				if scenario == "stale_marker" {
					marker = "other-run"
				}
				write("signature.complete", []byte(marker))
			}
			preamble := "set -eu\nadmission_phase=admit\nADMISSION_RUN_ID=fixture-run\nPOLICY_REVISION=1\nPOLICY_SHA256=" + digest + "\nEXPECTED_BUILD_TYPE=fixture\nEXPECTED_BUILDER_ID=fixture\nowner_scope_kind=ORGANIZATION\nowner_organization_ref=org_fixture01\nowner_project_ref=''\nimage_digest=sha256:" + digest + "\nspec_sha256=" + digest + "\nimmutable_build_sha256=" + digest + "\nstaging_host=registry.invalid\nsource_ref=registry.invalid/fixture\nfail() { echo \"$1\" >&2; exit 1; }\nlogin_registry() { fail 'fixture registry unavailable'; }\n"
			code := "SCAN_PREDECESSOR_FAILED"
			if scenario == "unknown_phase" {
				code = "UNKNOWN"
			}
			program := preamble + functions("write_marker") + functions("wait_for_file") + functions("wait_for_marker") + functions("write_technical_rejection") + functions("reject_failed_predecessor") + "\nreject_failed_predecessor " + code + "\n"
			program = strings.ReplaceAll(program, "/work/", work+"/")
			program = strings.ReplaceAll(program, "image-tool-inventory-validator inventory", validator+" inventory")
			command := exec.CommandContext(t.Context(), "sh", "-c", program)
			output, err := command.CombinedOutput()
			positive := scenario == "evicted" || scenario == "existing_rejection"
			if positive && err != nil {
				t.Fatalf("recovery failed: %v: %s", err, output)
			}
			if !positive && err == nil {
				t.Fatal("invalid recovery accepted")
			}
			if positive {
				verdict, _ := os.ReadFile(filepath.Join(work, "verdict"))
				if strings.TrimSpace(string(verdict)) != "REJECTED" {
					t.Fatal("recovery accepted unsafe image")
				}
				if scenario == "evicted" {
					if _, err := os.Stat(filepath.Join(work, "signature.complete")); !os.IsNotExist(err) {
						t.Fatal("recovery fabricated predecessor marker")
					}
				}
			}
			stored, _ := os.ReadFile(filepath.Join(work, "tool-inventory.json"))
			if scenario != "missing_inventory" && string(stored) != string(raw) {
				t.Fatal("recovery rewrote actual inventory")
			}
		})
	}
}

func TestFailedPredecessorRestoresActualInventoryFromPinnedOCI(t *testing.T) {
	bin := t.TempDir()
	validator := filepath.Join(bin, "image-tool-inventory-validator")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", validator, "./cmd/image-tool-inventory-validator")
	build.Dir = filepath.Join(repositoryRoot(), "services/jobs/role-image-builder")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("validator build: %v: %s", err, output)
	}
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
		return string(source)[start : start+end+3]
	}
	for _, scenario := range []string{"missing_inventory", "missing_hash", "missing_provenance", "registry_unavailable", "digest_mismatch", "invalid_manifest", "foreign_labels", "foreign_native_provenance"} {
		t.Run(scenario, func(t *testing.T) {
			work := t.TempDir()
			write := func(name string, raw []byte) {
				if err := os.WriteFile(filepath.Join(work, name), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			jsonFile := func(name string, value any) {
				raw, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				write(name, raw)
			}
			digest := strings.Repeat("a", 64)
			platformDigest := "sha256:" + strings.Repeat("b", 64)
			attestationDigest := "sha256:" + strings.Repeat("c", 64)
			layerDigest := "sha256:" + strings.Repeat("d", 64)
			provenance := `{"buildType":"fixture","builderId":"fixture","immutableBuildSHA256":"` + digest + `","manifestDigest":"sha256:` + digest + `","organizationRef":"org_fixture01","policyRevision":1,"policySHA256":"` + digest + `","projectRef":"","schema":"kodex.dev/image-provenance-binding/v2","scopeKind":"ORGANIZATION","specSHA256":"` + digest + `"}`
			sum := sha256.Sum256([]byte(provenance))
			provenanceSHA := hex.EncodeToString(sum[:])
			jsonFile("owner-claim.json", map[string]any{"artifactId": "imgart_fixture01", "manifestDigest": "sha256:" + digest, "specSHA256": digest, "immutableBuildSHA256": digest, "provenanceSHA256": provenanceSHA, "policyRevision": 1, "policySHA256": digest, "declaredTools": []any{}})
			jsonFile("fixture-index.json", map[string]any{"manifests": []any{map[string]any{"digest": platformDigest, "platform": map[string]string{"os": "linux", "architecture": "amd64"}}, map[string]any{"digest": attestationDigest, "platform": map[string]string{"os": "unknown", "architecture": "unknown"}, "annotations": map[string]string{"vnd.docker.reference.digest": platformDigest}}}})
			jsonFile("fixture-provenance-manifest.json", map[string]any{"layers": []any{map[string]string{"mediaType": "application/vnd.in-toto+json", "digest": layerDigest}}})
			labels := map[string]string{"kodex.dev/spec-sha256": digest, "kodex.dev/immutable-build-sha256": digest, "kodex.dev/source-sha256": digest, "kodex.dev/context-sha256": digest, "kodex.dev/base-image-digest": "sha256:" + digest, "kodex.dev/builder-sha256": digest, "kodex.dev/frontend-sha256": digest, "kodex.dev/toolchain-sha256": digest, "kodex.dev/policy-revision": "1", "kodex.dev/policy-sha256": digest, "kodex.dev/runtime-contract-sha256": digest}
			if scenario == "foreign_labels" {
				labels["kodex.dev/spec-sha256"] = strings.Repeat("b", 64)
			}
			jsonFile("fixture-labels.json", labels)
			jsonFile("fixture-config.json", map[string]any{"User": "10001:10001", "Entrypoint": []string{"/usr/local/bin/kodex-init", "entrypoint", "/usr/local/bin/kodex-agent-runner"}, "Cmd": []string{"runtime-session"}})
			nativeBuilder := "fixture"
			if scenario == "foreign_native_provenance" {
				nativeBuilder = "foreign"
			}
			jsonFile("fixture-native.json", map[string]any{"_type": "https://in-toto.io/Statement/v0.1", "predicateType": "https://slsa.dev/provenance/v1", "subject": []any{map[string]any{"name": "registry.invalid/fixture", "digest": map[string]string{"sha256": strings.TrimPrefix(platformDigest, "sha256:")}}}, "predicate": map[string]any{"buildDefinition": map[string]any{"buildType": "fixture", "resolvedDependencies": []any{map[string]any{"uri": "docker-image://registry.invalid/base", "digest": map[string]string{"sha256": digest}}}}, "runDetails": map[string]any{"builder": map[string]string{"id": nativeBuilder}}}})
			manifest := runtimecontract.ImageToolManifest{Schema: runtimecontract.ImageInventorySchema, SpecSHA256: digest, ImmutableBuildSHA256: digest, RuntimeContractSHA256: digest, Platform: "linux/amd64"}
			for _, probe := range runtimecontract.ImageToolProbes() {
				manifest.Tools = append(manifest.Tools, runtimecontract.ImageToolObservation{Name: probe.Name, Status: "MISSING", Required: probe.Required})
			}
			jsonFile("fixture-tools.json", manifest)
			if scenario == "invalid_manifest" {
				write("fixture-tools.json", []byte(`{"unknown":true}`))
			}
			write("expected-platforms", []byte("linux/amd64\n"))
			if scenario == "missing_hash" || scenario == "missing_provenance" {
				rawManifest, _ := json.Marshal(manifest)
				inventory := runtimecontract.ImageToolInventory{Schema: runtimecontract.ImageInventoryBindingSchema, ImageDigest: "sha256:" + digest, ProvenanceSHA256: provenanceSHA, Platforms: []runtimecontract.ImagePlatformInventory{{PlatformDigest: platformDigest, ManifestSHA256: runtimecontract.ImageInventorySHA256(rawManifest), Manifest: manifest}}}
				raw, _ := json.Marshal(inventory)
				write("tool-inventory.json", raw)
				sum := sha256.Sum256(raw)
				write("tool-inventory.sha256", []byte(hex.EncodeToString(sum[:])))
				if scenario == "missing_hash" {
					if err := os.Remove(filepath.Join(work, "tool-inventory.sha256")); err != nil {
						t.Fatal(err)
					}
					write("provenance.json", []byte(provenance))
					write("provenance.sha256", []byte(provenanceSHA))
				}
			}
			preamble := "set -eu\nadmission_phase=admit\nADMISSION_RUN_ID=fixture-run\nPOLICY_REVISION=1\nPOLICY_SHA256=" + digest + "\nEXPECTED_BUILD_TYPE=fixture\nEXPECTED_BUILDER_ID=fixture\nowner_scope_kind=ORGANIZATION\nowner_organization_ref=org_fixture01\nowner_project_ref=''\nimage_digest=sha256:" + digest + "\nspec_sha256=" + digest + "\nimmutable_build_sha256=" + digest + "\nexpected_provenance_sha256=" + provenanceSHA + "\nsource_sha256=" + digest + "\ncontext_sha256=" + digest + "\nbase_image_digest=sha256:" + digest + "\nbuilder_sha256=" + digest + "\nfrontend_sha256=" + digest + "\ntoolchain_sha256=" + digest + "\nruntime_contract_sha256=" + digest + "\nstaging_host=registry.invalid\nsubject_name=registry.invalid/fixture\nsource_ref=registry.invalid/fixture@sha256:" + digest + "\nSCENARIO=" + scenario + "\nfail() { echo \"$1\" >&2; exit 1; }\n"
			registry := `login_registry() {
  [ "$1" = registry.invalid ] && [ "$2" = /identity/username ] && [ "$3" = /identity/password ] || fail 'unexpected fixture credential path'
  printf login > /work/registry-login
}
regctl() {
  printf '%s\n' "$1/$2" >> /work/registry-reads
  case "$1/$2" in
    image/digest)
      [ "$3" = "$source_ref" ] || exit 1
      [ "$SCENARIO" != registry_unavailable ] || exit 1
      if [ "$SCENARIO" = digest_mismatch ]; then printf 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'; else printf '%s' "$image_digest"; fi ;;
    manifest/get)
      if [ "$3" = "$source_ref" ]; then cat /work/fixture-index.json
      elif [ "$3" = "$subject_name@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" ]; then cat /work/fixture-provenance-manifest.json
      else exit 1; fi ;;
    image/inspect)
      [ "$3" = "$subject_name@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ] || exit 1
      if [ "$5" = '{{json .Config.Labels}}' ]; then cat /work/fixture-labels.json
      elif [ "$5" = '{{json .Config}}' ]; then cat /work/fixture-config.json
      else exit 1; fi ;;
    blob/get)
      [ "$3" = "$source_ref" ] && [ "$4" = sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd ] || exit 1
      cat /work/fixture-native.json ;;
    image/get-file)
      [ "$3" = "$subject_name@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ] && [ "$4" = /usr/share/kodex/tool-inventory.json ] || exit 1
      cat /work/fixture-tools.json ;;
    *) exit 1 ;;
  esac
}
`
			program := preamble + registry + function("write_marker") + function("wait_for_file") + function("wait_for_marker") + function("verify_runtime_config") + function("verify_image_and_provenance") + function("write_technical_rejection") + function("reject_failed_predecessor") + "\nreject_failed_predecessor SCAN_PREDECESSOR_FAILED\n"
			program = strings.ReplaceAll(program, "/work/", work+"/")
			program = strings.ReplaceAll(program, "image-tool-inventory-validator", validator)
			program = strings.ReplaceAll(program, "/opt/kodex/provenance-policy.jq", filepath.Join(repositoryRoot(), "deploy/k8s/base/image-supply-chain/provenance-policy.jq"))
			command := exec.CommandContext(t.Context(), "sh", "-c", program)
			output, err := command.CombinedOutput()
			positive := strings.HasPrefix(scenario, "missing_")
			if positive && err != nil {
				t.Fatalf("pinned inventory restore failed: %v: %s", err, output)
			}
			if !positive && err == nil {
				t.Fatal("unsafe registry evidence accepted")
			}
			verdict, _ := os.ReadFile(filepath.Join(work, "verdict"))
			if positive {
				if strings.TrimSpace(string(verdict)) != "REJECTED" {
					t.Fatal("recovery accepted image without scan/sign")
				}
				raw, err := os.ReadFile(filepath.Join(work, "tool-inventory.json"))
				if err != nil {
					t.Fatal(err)
				}
				bound, err := runtimecontract.DecodeImageToolInventory(raw)
				if err != nil || bound.ImageDigest != "sha256:"+digest || bound.ProvenanceSHA256 != provenanceSHA || bound.Platforms[0].PlatformDigest != platformDigest {
					t.Fatal("restored owner pins invalid")
				}
				storedSHA, _ := os.ReadFile(filepath.Join(work, "tool-inventory.sha256"))
				sum := sha256.Sum256(raw)
				if strings.TrimSpace(string(storedSHA)) != hex.EncodeToString(sum[:]) {
					t.Fatal("restored inventory hash invalid")
				}
				for _, marker := range []string{"scan.complete", "signature.complete"} {
					if _, err := os.Stat(filepath.Join(work, marker)); !os.IsNotExist(err) {
						t.Fatal("recovery fabricated stage marker")
					}
				}
			} else if len(verdict) > 0 {
				t.Fatal("invalid evidence produced owner outcome")
			}
			reads, _ := os.ReadFile(filepath.Join(work, "registry-reads"))
			if strings.Contains(string(reads), "syft") || strings.Contains(string(reads), "cosign") {
				t.Fatal("read-only restoration invoked scan/sign")
			}
		})
	}
}

package admissioncontroller

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/ext"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type admissionPolicyDocument struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		MatchConditions []struct {
			Name       string `json:"name"`
			Expression string `json:"expression"`
		} `json:"matchConditions"`
		Variables []struct {
			Name       string `json:"name"`
			Expression string `json:"expression"`
		} `json:"variables"`
		Validations []struct {
			Expression string `json:"expression"`
		} `json:"validations"`
	} `json:"spec"`
}

func TestAdmissionPoliciesContainSyntacticallyValidCEL(t *testing.T) {
	policies, err := readAdmissionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policies {
		compilePolicyExpressions(t, policy)
	}
	if len(policies) != 2 {
		t.Fatalf("expected two controller admission policies, got %d", len(policies))
	}
}

func TestAdmissionPoliciesAcceptExactRenderedResources(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to execute the production renderer")
	}
	policies, err := readAdmissionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]admissionPolicyDocument, len(policies))
	for _, policy := range policies {
		byName[policy.Metadata.Name] = policy
	}
	ownerPolicy := completeTestPolicy()
	renderer, err := NewScriptRenderer(filepath.Join(repositoryRoot(), "tools", "render-image-admission-job.sh"))
	if err != nil {
		t.Fatal(err)
	}
	runID := "v20260822120000-" + testOrchestrationRevision
	for _, phase := range phases {
		rendered, err := renderer.Render(t.Context(), ownerPolicy, "production", runID, phase)
		if err != nil {
			t.Fatalf("render %s: %v", phase, err)
		}
		if err := prepareRendered(rendered, "mattercodex-system", runID, phase); err != nil {
			t.Fatalf("prepare %s: %v", phase, err)
		}
		job, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rendered.Job)
		if err != nil {
			t.Fatal(err)
		}
		assertPolicyAccepts(t, byName["mattercodex-image-admission-controller-jobs"], job, ownerPolicy)
		if rendered.PVC != nil {
			workspace, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rendered.PVC)
			if err != nil {
				t.Fatal(err)
			}
			assertPolicyAccepts(t, byName["mattercodex-image-admission-controller-workspaces"], workspace, ownerPolicy)
		}
	}
}

func TestAdmissionJobPolicyRejectsPrivilegeExpansion(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to execute the production renderer")
	}
	policies, err := readAdmissionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	ownerPolicy := completeTestPolicy()
	renderer, err := NewScriptRenderer(filepath.Join(repositoryRoot(), "tools", "render-image-admission-job.sh"))
	if err != nil {
		t.Fatal(err)
	}
	runID := "v20260822120000-" + testOrchestrationRevision
	rendered, err := renderer.Render(t.Context(), ownerPolicy, "production", runID, "claim")
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareRendered(rendered, "mattercodex-system", runID, "claim"); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*batchv1.Job){
		"service account": func(job *batchv1.Job) {
			job.Spec.Template.Spec.ServiceAccountName = "cluster-admin"
		},
		"executable image": func(job *batchv1.Job) {
			job.Spec.Template.Spec.Containers[0].Image = "registry.evil.invalid/image@sha256:" + stringsOf("f", 64)
		},
		"secret volume": func(job *batchv1.Job) {
			job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes,
				corev1.Volume{Name: "foreign-secret", VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: "foreign-secret"},
				}})
		},
		"authority endpoint": func(job *batchv1.Job) {
			for index := range job.Spec.Template.Spec.InitContainers[1].Env {
				item := &job.Spec.Template.Spec.InitContainers[1].Env[index]
				if item.Name == "INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_ADDRESS" {
					item.Value = "foreign-service.mattercodex-system.svc:8443"
				}
			}
		},
	}
	for _, policy := range policies {
		if policy.Metadata.Name == "mattercodex-image-admission-controller-jobs" {
			for name, mutate := range mutations {
				t.Run(name, func(t *testing.T) {
					job := rendered.Job.DeepCopy()
					mutate(job)
					unstructuredJob, err := runtime.DefaultUnstructuredConverter.ToUnstructured(job)
					if err != nil {
						t.Fatal(err)
					}
					assertPolicyRejects(t, policy, unstructuredJob, ownerPolicy)
				})
			}
			return
		}
	}
	t.Fatal("job policy is missing")
}

func repositoryRoot() string {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		panic(err)
	}
	return root
}

func readAdmissionPolicies() ([]admissionPolicyDocument, error) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(), "deploy", "k8s", "base",
		"image-supply-chain", "image-admission-controller-policy.yaml"))
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 64<<10)
	var policies []admissionPolicyDocument
	for {
		var document json.RawMessage
		if err := decoder.Decode(&document); errors.Is(err, io.EOF) {
			return policies, nil
		} else if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(document)) == 0 {
			continue
		}
		var policy admissionPolicyDocument
		if err := json.Unmarshal(document, &policy); err != nil {
			return nil, err
		}
		if policy.Kind == "ValidatingAdmissionPolicy" {
			policies = append(policies, policy)
		}
	}
}

func completeTestPolicy() *corev1.ConfigMap {
	immutable := true
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: policyName, Namespace: "mattercodex-system",
		Labels:      map[string]string{"mattercodex.dev/owner-intent": "true"},
		Annotations: map[string]string{"mattercodex.dev/admission-tools-sha256": "sha256:" + stringsOf("e", 64)},
	}, Immutable: &immutable, Data: map[string]string{
		"orchestrationRevision":       testOrchestrationRevision,
		"toolsImage":                  "registry.example.test/mattercodex/admission-tools@sha256:" + stringsOf("e", 64),
		"admissionImage":              "registry.example.test/mattercodex/image-admission@sha256:" + stringsOf("f", 64),
		"authorityImage":              "registry.example.test/mattercodex/internal-rpc-authority@sha256:" + stringsOf("b", 64),
		"promotionRepository":         "mattercodex-image-registry-promotion.mattercodex-system.svc.cluster.local:5003/mattercodex/roles",
		"promotionEvidenceRepository": "mattercodex-image-registry-promotion.mattercodex-system.svc.cluster.local:5003/mattercodex/evidence",
		"evidenceRepository":          "mattercodex-image-registry-evidence.mattercodex-system.svc.cluster.local:5007/evidence/role-image-admission",
		"promotedPullRepository":      "registry.example.test/mattercodex/roles",
		"policyRevision":              "7", "policySHA256": stringsOf("c", 64),
		"builderIdentity":             "spiffe://mattercodex.local/ns/mattercodex-system/sa/role-image-builder",
		"buildType":                   "https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md",
		"trustedRoleBaseRepository":   "registry.example.test/mattercodex/agent-runner",
		"trustedRoleBaseDigest":       "sha256:" + stringsOf("a", 64),
		"roleRuntimeContractRevision": "1", "roleRuntimeContractSHA256": stringsOf("d", 64),
		"requiredTools": "base64,cmp,cosign,grype,image-admission-bridge,jq,regctl,sha256sum,syft,wc",
	}}
}

func stringsOf(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}

func compilePolicyExpressions(t *testing.T, policy admissionPolicyDocument) {
	t.Helper()
	environment, err := newPolicyEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	compile := func(name, expression string) {
		t.Helper()
		_, issues := environment.Compile(expression)
		if issues != nil && issues.Err() != nil {
			t.Fatalf("CEL %s is invalid: %v", name, issues.Err())
		}
	}
	for _, condition := range policy.Spec.MatchConditions {
		compile("match condition "+condition.Name, condition.Expression)
	}
	for _, variable := range policy.Spec.Variables {
		compile("variable "+variable.Name, variable.Expression)
	}
	for index, validation := range policy.Spec.Validations {
		compile("validation "+string(rune('A'+index)), validation.Expression)
	}
}

func assertPolicyAccepts(t *testing.T, policy admissionPolicyDocument, object map[string]any, ownerPolicy *corev1.ConfigMap) {
	t.Helper()
	accepted, err := evaluateAdmissionPolicy(policy, object, ownerPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if !accepted {
		t.Fatalf("exact resource was rejected by %s", policy.Metadata.Name)
	}
}

func assertPolicyRejects(t *testing.T, policy admissionPolicyDocument, object map[string]any, ownerPolicy *corev1.ConfigMap) {
	t.Helper()
	accepted, err := evaluateAdmissionPolicy(policy, object, ownerPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if accepted {
		t.Fatalf("privilege expansion was accepted by %s", policy.Metadata.Name)
	}
}

func evaluateAdmissionPolicy(policy admissionPolicyDocument, object map[string]any, ownerPolicy *corev1.ConfigMap) (bool, error) {
	environment, err := newPolicyEnvironment()
	if err != nil {
		return false, err
	}
	parameterSpec := make(map[string]any, len(ownerPolicy.Data))
	for name, value := range ownerPolicy.Data {
		parameterSpec[name] = value
	}
	params := map[string]any{
		"apiVersion": "supplychain.mattercodex.dev/v1alpha1",
		"kind":       "ImageAdmissionPolicyParameters",
		"metadata": map[string]any{
			"name":      ownerPolicy.Name,
			"namespace": ownerPolicy.Namespace,
		},
		"spec": parameterSpec,
	}
	variableValues := map[string]any{}
	activation := map[string]any{
		"object": object, "oldObject": nil, "params": params,
		"request": map[string]any{"userInfo": map[string]any{
			"username": "system:serviceaccount:mattercodex-system:image-admission-controller",
		}},
		"variables": variableValues,
	}
	evaluate := func(expression string) (types.Bool, error) {
		ast, issues := environment.Compile(expression)
		if issues != nil && issues.Err() != nil {
			return false, issues.Err()
		}
		program, err := environment.Program(ast)
		if err != nil {
			return false, err
		}
		result, _, err := program.Eval(activation)
		if err != nil {
			return false, err
		}
		boolean, ok := result.(types.Bool)
		if !ok {
			return false, errors.New("admission policy expression did not return bool")
		}
		return boolean, nil
	}
	for _, condition := range policy.Spec.MatchConditions {
		matched, err := evaluate(condition.Expression)
		if err != nil || matched != types.True {
			return false, err
		}
	}
	for _, variable := range policy.Spec.Variables {
		ast, issues := environment.Compile(variable.Expression)
		if issues != nil && issues.Err() != nil {
			return false, issues.Err()
		}
		program, err := environment.Program(ast)
		if err != nil {
			return false, err
		}
		result, _, err := program.Eval(activation)
		if err != nil {
			return false, err
		}
		variableValues[variable.Name] = result.Value()
	}
	for _, validation := range policy.Spec.Validations {
		valid, err := evaluate(validation.Expression)
		if err != nil {
			return false, err
		}
		if valid != types.True {
			return false, nil
		}
	}
	return true, nil
}

func newPolicyEnvironment() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("object", cel.DynType),
		cel.Variable("oldObject", cel.DynType),
		cel.Variable("params", cel.DynType),
		cel.Variable("request", cel.DynType),
		cel.Variable("variables", cel.DynType),
		ext.Strings(),
	)
}

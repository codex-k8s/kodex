package admissioncontroller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

// admissionPolicyDocument decodes only the policy identity and CEL expression
// lists needed by local tests; it is not a complete Kubernetes policy schema or
// a strict unknown-field decoder and is never used to authorize runtime requests.
// admissionPolicyDocument разбирает лишь identity политики и списки CEL для
// локальных тестов; это не полная Kubernetes schema, не strict unknown-field
// decoder и не средство выдачи полномочий runtime-запросам.
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

// TestAdmissionPoliciesContainSyntacticallyValidCEL compiles every match condition,
// variable and validation from the three repository policies in the local dynamic
// CEL environment; it does not install them or exercise API-server admission.
// TestAdmissionPoliciesContainSyntacticallyValidCEL компилирует все match conditions,
// variables и validations трёх политик репозитория в локальном dynamic CEL;
// установка политик и admission настоящего API server не выполняются.
func TestAdmissionPoliciesContainSyntacticallyValidCEL(t *testing.T) {
	policies, err := readAdmissionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policies {
		compilePolicyExpressions(t, policy)
	}
	if len(policies) != 3 {
		t.Fatalf("expected three controller admission policies, got %d", len(policies))
	}
}

// TestAdmissionPoliciesAcceptExactRenderedResources renders/prepares every phase
// with synthetic owner policy, requires zero Job retries and accepts the exact
// Job/PVC through local CEL evaluation. Missing jq skips this test, not admission
// enforcement, and rendered resources are never applied to a cluster.
// TestAdmissionPoliciesAcceptExactRenderedResources создаёт и подготавливает все
// фазы с искусственной owner policy, требует ноль повторов Job и принятия точных
// Job/PVC локальным CEL. Отсутствие jq пропускает этот тест, а не enforcement;
// созданные ресурсы никогда не применяются к кластеру.
func TestAdmissionPoliciesAcceptExactRenderedResources(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to execute the production renderer")
	}
	policies, err := readAdmissionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	byName := admissionPoliciesByName(policies)
	ownerPolicy := completeTestPolicy()
	renderer := newTestScriptRenderer(t)
	runID := "v20260822120000-" + testOrchestrationRevision
	for _, phase := range phases {
		rendered, err := renderer.Render(t.Context(), ownerPolicy, "production", runID, phase)
		if err != nil {
			t.Fatalf("render %s: %v", phase, err)
		}
		if err := prepareRendered(rendered, "kodex-system", runID, phase); err != nil {
			t.Fatalf("prepare %s: %v", phase, err)
		}
		if rendered.Job.Spec.BackoffLimit == nil || *rendered.Job.Spec.BackoffLimit != 0 {
			t.Fatalf("phase %s must delegate retries to the owner controller", phase)
		}
		job, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rendered.Job)
		if err != nil {
			t.Fatal(err)
		}
		assertPolicyAccepts(t, byName["kodex-image-admission-controller-jobs"], job, ownerPolicy)
		if rendered.PVC != nil {
			workspace, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rendered.PVC)
			if err != nil {
				t.Fatal(err)
			}
			assertPolicyAccepts(t, byName["kodex-image-admission-controller-workspaces"], workspace, ownerPolicy)
		}
	}
}

// TestAdmissionJobPolicyRejectsPrivilegeExpansion deep-copies the rendered claim
// Job and separately changes service account, executable image, secret volume or
// authority endpoint. Each must evaluate to false without a CEL error; the source
// fixture stays intact, and absent job policy fails the test explicitly.
// TestAdmissionJobPolicyRejectsPrivilegeExpansion глубоко копирует claim Job
// и отдельно меняет service account, образ, secret volume либо authority endpoint.
// Каждый вариант должен дать false без CEL-ошибки; исходная fixture сохраняется,
// а отсутствие job policy явно завершает тест ошибкой.
func TestAdmissionJobPolicyRejectsPrivilegeExpansion(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to execute the production renderer")
	}
	policies, err := readAdmissionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	ownerPolicy := completeTestPolicy()
	renderer := newTestScriptRenderer(t)
	runID := "v20260822120000-" + testOrchestrationRevision
	rendered, err := renderer.Render(t.Context(), ownerPolicy, "production", runID, "claim")
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareRendered(rendered, "kodex-system", runID, "claim"); err != nil {
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
					item.Value = "foreign-service.kodex-system.svc:8443"
				}
			}
		},
	}
	for _, policy := range policies {
		if policy.Metadata.Name == "kodex-image-admission-controller-jobs" {
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

// TestAdmissionReportMaterializationHasExactRegistryAndAdmitPaths evaluates rendered
// Jobs in protected/trusted profiles, confines report inputs to admit and rejects
// altered paths, validator entrypoint and non-exact tool registries. It checks local
// shell/CEL agreement, not scanner execution, signed evidence or a real image verdict.
// TestAdmissionReportMaterializationHasExactRegistryAndAdmitPaths проверяет Job
// protected/trusted-профилей, ограничивает report inputs фазой admit и отклоняет
// чужие пути, validator entrypoint и неточный реестр инструментов. Проверяется
// согласованность shell/CEL, не сканирование, signed evidence и реальный допуск.
func TestAdmissionReportMaterializationHasExactRegistryAndAdmitPaths(t *testing.T) {
	policies, err := readAdmissionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	var jobPolicy admissionPolicyDocument
	for _, policy := range policies {
		if policy.Metadata.Name == "kodex-image-admission-controller-jobs" {
			jobPolicy = policy
		}
	}
	renderer := newTestScriptRenderer(t)
	runID := "v20261005120000-" + testOrchestrationRevision
	for _, profile := range []string{"protected", "trusted-cluster"} {
		ownerPolicy := completeTestPolicy()
		if profile == "trusted-cluster" {
			ownerPolicy.Labels["kodex.dev/security-profile"] = profile
		}
		for _, phase := range phases {
			rendered, err := renderer.Render(t.Context(), ownerPolicy, "production", runID, phase)
			if err != nil {
				t.Fatal(err)
			}
			if err := prepareRendered(rendered, "kodex-system", runID, phase); err != nil {
				t.Fatal(err)
			}
			job, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rendered.Job)
			if err != nil {
				t.Fatal(err)
			}
			assertPolicyAccepts(t, jobPolicy, job, ownerPolicy)
			for _, name := range []string{"IMAGE_OWNER_VULNERABILITY_REPORT_JSON_FILE", "IMAGE_OWNER_VULNERABILITY_REPORT_PROJECTION_SHA256_FILE"} {
				found := false
				for _, item := range rendered.Job.Spec.Template.Spec.Containers[0].Env {
					found = found || item.Name == name
				}
				if found != (phase == "admit") {
					t.Fatal("report paths escaped admit phase")
				}
			}
			if phase != "admit" {
				continue
			}
			for _, name := range []string{"IMAGE_OWNER_VULNERABILITY_REPORT_JSON_FILE", "IMAGE_OWNER_VULNERABILITY_REPORT_PROJECTION_SHA256_FILE"} {
				changed := rendered.Job.DeepCopy()
				for index := range changed.Spec.Template.Spec.Containers[0].Env {
					item := &changed.Spec.Template.Spec.Containers[0].Env[index]
					if item.Name == name {
						item.Value = "/work/foreign.json"
					}
				}
				object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(changed)
				if err != nil {
					t.Fatal(err)
				}
				assertPolicyRejects(t, jobPolicy, object, ownerPolicy)
			}
			changed := rendered.Job.DeepCopy()
			changed.Spec.Template.Spec.Containers[0].Command = []string{"/usr/local/bin/image-vulnerability-report-validator", "report"}
			object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(changed)
			if err != nil {
				t.Fatal(err)
			}
			assertPolicyRejects(t, jobPolicy, object, ownerPolicy)
			for _, tools := range []string{
				"base64,cmp,cosign,grype,image-admission-bridge,image-tool-inventory-validator,jq,regctl,sha256sum,syft,tr,wc",
				ownerPolicy.Data["requiredTools"] + ",foreign-helper",
				"image-vulnerability-report-validator," + ownerPolicy.Data["requiredTools"],
			} {
				invalid := ownerPolicy.DeepCopy()
				invalid.Data["requiredTools"] = tools
				assertPolicyRejects(t, jobPolicy, job, invalid)
				if _, err := renderer.Render(t.Context(), invalid, "production", runID, phase); err == nil {
					t.Fatal("renderer accepted non-exact executable registry")
				}
			}
		}
	}
}

// TestAdmissionPoliciesAcceptOnlyExactProofHoldAndRelease accepts a synthetic held
// Job and its exact unsuspend update with oldObject, then rejects a simultaneous
// service-account expansion. UID/resourceVersion here are fixture values, not live
// reservations, and jq absence remains the original explicit test skip.
// TestAdmissionPoliciesAcceptOnlyExactProofHoldAndRelease принимает искусственный
// held Job и точное снятие suspend с oldObject, затем отклоняет одновременную замену
// service account. UID/resourceVersion — данные fixture, не живые reservations;
// отсутствие jq сохраняет прежний явный пропуск теста.
func TestAdmissionPoliciesAcceptOnlyExactProofHoldAndRelease(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is required to execute the production renderer")
	}
	policies, err := readAdmissionPolicies()
	if err != nil {
		t.Fatal(err)
	}
	byName := admissionPoliciesByName(policies)
	ownerPolicy := completeTestPolicy()
	renderer := newTestScriptRenderer(t)
	runID := "v20260909120000-" + testOrchestrationRevision
	rendered, err := renderer.Render(t.Context(), ownerPolicy, "production", runID, "claim")
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareRendered(rendered, "kodex-system", runID, "claim"); err != nil {
		t.Fatal(err)
	}
	prepareProofReservation(rendered.Job, runID, "claim")
	rendered.Job.UID = "11111111-1111-4111-8111-111111111111"
	rendered.Job.ResourceVersion = "7"
	held, err := runtime.DefaultUnstructuredConverter.ToUnstructured(rendered.Job)
	if err != nil {
		t.Fatal(err)
	}
	assertPolicyAccepts(t, byName["kodex-image-admission-controller-jobs"], held, ownerPolicy)
	released := rendered.Job.DeepCopy()
	value := false
	released.Spec.Suspend = &value
	releasedObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(released)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := evaluateAdmissionPolicyWithOld(byName["kodex-image-admission-proof-release"], releasedObject, held, ownerPolicy)
	if err != nil || !accepted {
		t.Fatalf("exact release was rejected: accepted=%v err=%v", accepted, err)
	}
	released.Spec.Template.Spec.ServiceAccountName = "cluster-admin"
	changed, err := runtime.DefaultUnstructuredConverter.ToUnstructured(released)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err = evaluateAdmissionPolicyWithOld(byName["kodex-image-admission-proof-release"], changed, held, ownerPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if accepted {
		t.Fatal("release policy accepted a changed Pod template")
	}
}

// newTestScriptRenderer constructs the existing repository-owned shell renderer
// with the same path/error handling. It neither runs the script nor inserts jq
// skips, render calls, policy inputs or phase preparation into a caller's test.
// newTestScriptRenderer создаёт существующий shell renderer из репозитория
// с прежними путём и проверкой ошибки. Скрипт, jq skips, render, policy и подготовка
// фаз остаются в вызывающем тесте и здесь не выполняются.
func newTestScriptRenderer(t *testing.T) *ScriptRenderer {
	t.Helper()
	renderer, err := NewScriptRenderer(filepath.Join(repositoryRoot(), "tools", "render-image-admission-job.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return renderer
}

// admissionPoliciesByName builds a fresh shallow index with the same last-name-wins
// behavior as the two original loops; it does not validate, deduplicate or reorder
// the source list used by tests that intentionally inspect its first match.
// admissionPoliciesByName строит новый неглубокий индекс с прежним приоритетом
// последнего одинакового имени. Исходный список не проверяется, не очищается
// от дублей и не переставляется для тестов, выбирающих первое совпадение.
func admissionPoliciesByName(policies []admissionPolicyDocument) map[string]admissionPolicyDocument {
	byName := make(map[string]admissionPolicyDocument, len(policies))
	for _, policy := range policies {
		byName[policy.Metadata.Name] = policy
	}
	return byName
}

// TestAdmissionPolicyIndexPreservesLastMatchAndInput checks the original duplicate
// overwrite rule, independent map storage and an allocated empty result for nil
// input, without interpreting policy expressions or changing list order.
// TestAdmissionPolicyIndexPreservesLastMatchAndInput проверяет прежнюю замену
// одинакового имени, независимость map и выделенный пустой результат для nil,
// не вычисляя выражения политики и не меняя порядок исходного списка.
func TestAdmissionPolicyIndexPreservesLastMatchAndInput(t *testing.T) {
	first := admissionPolicyDocument{Kind: "first"}
	first.Metadata.Name = "duplicate"
	second := admissionPolicyDocument{Kind: "second"}
	second.Metadata.Name = "other"
	last := admissionPolicyDocument{Kind: "last"}
	last.Metadata.Name = "duplicate"
	policies := []admissionPolicyDocument{first, second, last}
	byName := admissionPoliciesByName(policies)
	if len(byName) != 2 || byName["duplicate"].Kind != "last" || byName["other"].Kind != "second" {
		t.Fatal("policy index changed duplicate resolution")
	}
	delete(byName, "other")
	byName["duplicate"] = first
	if len(policies) != 3 || policies[1].Metadata.Name != "other" || policies[2].Kind != "last" {
		t.Fatal("policy index mutated source list")
	}
	if empty := admissionPoliciesByName(nil); empty == nil || len(empty) != 0 {
		t.Fatal("empty policy index changed allocation behavior")
	}
}

// repositoryRoot resolves the fixed five-parent path from this package's test
// directory to the checkout root and panics if absolute-path resolution fails;
// it does not search arbitrary directories or discover external installations.
// repositoryRoot переводит фиксированный путь на пять уровней вверх от тестового
// пакета в абсолютный корень checkout и паникует при ошибке разрешения; произвольные
// каталоги и внешние установки не разыскиваются.
func repositoryRoot() string {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		panic(err)
	}
	return root
}

// readAdmissionPolicies reads the fixed repository manifest, skips empty/non-policy
// documents and preserves policy order while propagating read/decode failures.
// The 64KiB YAML decoder setting is a buffer size, not a total input-size limit;
// this fixture loader does not perform runtime trust or strict schema validation.
// readAdmissionPolicies читает фиксированный manifest репозитория, пропускает пустые
// и не-policy документы, сохраняя порядок политик и ошибки чтения/разбора. 64KiB
// YAML decoder — размер буфера, не общий лимит входа; загрузчик fixture не проверяет
// runtime trust и не выполняет strict schema validation.
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

// completeTestPolicy returns a fresh synthetic immutable policy with exact fixture
// digests, repositories and required tools for the real local renderer. Its owner-
// intent label, identities and image references grant no real approval or access,
// and the function neither reads private credentials nor contacts those endpoints.
// completeTestPolicy возвращает новую искусственную immutable policy с точными
// хешами, repositories и tools для настоящего локального renderer. Owner-intent,
// identity и image references не выдают реального approval либо доступа; функция
// не читает закрытые реквизиты и не обращается к указанным адресам.
func completeTestPolicy() *corev1.ConfigMap {
	immutable := true
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: policyName, Namespace: "kodex-system",
		Labels:      map[string]string{"kodex.dev/owner-intent": "true"},
		Annotations: map[string]string{"kodex.dev/admission-tools-sha256": "sha256:" + stringsOf("e", 64)},
	}, Immutable: &immutable, Data: map[string]string{
		"orchestrationRevision":       testOrchestrationRevision,
		"toolsImage":                  "registry.example.test/kodex/admission-tools@sha256:" + stringsOf("e", 64),
		"admissionImage":              "registry.example.test/kodex/image-admission@sha256:" + stringsOf("f", 64),
		"authorityImage":              "registry.example.test/kodex/internal-rpc-authority@sha256:" + stringsOf("b", 64),
		"promotionRepository":         "kodex-image-registry-promotion.kodex-system.svc.cluster.local:5003/kodex/roles",
		"promotionEvidenceRepository": "kodex-image-registry-promotion.kodex-system.svc.cluster.local:5003/kodex/evidence",
		"evidenceRepository":          "kodex-image-registry-evidence.kodex-system.svc.cluster.local:5007/evidence/role-image-admission",
		"promotedPullRepository":      "registry.example.test/kodex/roles",
		"policyRevision":              "7", "policySHA256": stringsOf("c", 64),
		"builderIdentity":             "spiffe://kodex.local/ns/kodex-system/sa/role-image-builder",
		"buildType":                   "https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md",
		"trustedRoleBaseRepository":   "registry.example.test/kodex/agent-runner",
		"trustedRoleBaseDigest":       "sha256:" + stringsOf("a", 64),
		"roleRuntimeContractRevision": "1", "roleRuntimeContractSHA256": stringsOf("d", 64),
		"requiredTools": "base64,cmp,cosign,grype,image-admission-bridge,image-tool-inventory-validator,image-vulnerability-report-validator,jq,regctl,sha256sum,syft,tr,wc",
	}}
}

// stringsOf concatenates the fixture string count times; zero or negative counts
// return empty, preserving the original integer-range behavior without validation.
// stringsOf соединяет строку fixture count раз; ноль и отрицательное значение дают
// пустую строку, сохраняя исходное поведение integer range без проверки аргумента.
func stringsOf(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}

// compilePolicyExpressions compiles all declared expression categories with named
// failure diagnostics and stops on CEL issues; it does not evaluate variables or
// validate resources, Kubernetes schema, policy actor membership or cluster state.
// compilePolicyExpressions компилирует все категории выражений с именованными
// ошибками и останавливается при CEL issues; variables не вычисляются, ресурсы,
// Kubernetes schema, actor membership и состояние кластера не проверяются.
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

// assertPolicyAccepts requires error-free true from the local policy evaluator;
// evaluation failure is not accepted even when a fixture expects an allowed resource.
// assertPolicyAccepts требует true без ошибки от локального evaluator; ошибка
// вычисления не принимается даже для ожидаемо разрешённого ресурса fixture.
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

// assertPolicyRejects requires error-free false, distinguishing a deliberate policy
// denial from a broken CEL evaluation. Tests for malformed field types explicitly
// inspect errors themselves instead of weakening this common assertion.
// assertPolicyRejects требует false без ошибки, отличая осознанный policy denial
// от сломанного CEL. Тесты повреждённых типов отдельно рассматривают ошибки,
// не ослабляя эту общую проверку.
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

// evaluateAdmissionPolicy evaluates a create-shaped synthetic resource by supplying
// nil oldObject to the fixed-controller actor path; it does not call Kubernetes.
// evaluateAdmissionPolicy проверяет искусственный create-ресурс с nil oldObject
// через путь фиксированного controller actor, без вызова Kubernetes.
func evaluateAdmissionPolicy(policy admissionPolicyDocument, object map[string]any, ownerPolicy *corev1.ConfigMap) (bool, error) {
	return evaluateAdmissionPolicyWithOld(policy, object, nil, ownerPolicy)
}

// evaluateAdmissionPolicyWithOld uses the synthetic admission-controller service
// account name and forwards oldObject to select CREATE/UPDATE evaluation. That name
// is test input, not authentication or proof of a service-account credential.
// evaluateAdmissionPolicyWithOld задаёт искусственное имя service account контроллера
// и передаёт oldObject для CREATE/UPDATE. Имя — вход теста, а не аутентификация
// либо доказательство наличия реквизитов service account.
func evaluateAdmissionPolicyWithOld(policy admissionPolicyDocument, object, oldObject map[string]any, ownerPolicy *corev1.ConfigMap) (bool, error) {
	return evaluateAdmissionPolicyForActor(policy, object, oldObject, ownerPolicy, "system:serviceaccount:kodex-system:image-admission-controller")
}

// evaluateAdmissionPolicyForActor builds synthetic object/params/request activation,
// checks conditions, evaluates variables in order and requires boolean validations.
// Compile/program/evaluation errors propagate; a false condition returns false here
// rather than emulating the API server's complete policy selection/failure lifecycle.
// The helper neither authenticates actor nor enforces Kubernetes RBAC or schema.
// evaluateAdmissionPolicyForActor строит искусственные object/params/request,
// проверяет conditions, по порядку вычисляет variables и требует boolean validations.
// Ошибки compile/program/evaluation возвращаются; false condition здесь даёт false,
// а не моделирует полный выбор policy/failure lifecycle API server. Helper
// не аутентифицирует actor и не применяет Kubernetes RBAC либо schema.
func evaluateAdmissionPolicyForActor(policy admissionPolicyDocument, object, oldObject map[string]any, ownerPolicy *corev1.ConfigMap, actor string) (bool, error) {
	environment, err := newPolicyEnvironment()
	if err != nil {
		return false, err
	}
	parameterSpec := make(map[string]any, len(ownerPolicy.Data))
	for name, value := range ownerPolicy.Data {
		parameterSpec[name] = value
	}
	params := map[string]any{
		"apiVersion": "supplychain.kodex.dev/v1alpha1",
		"kind":       "ImageAdmissionPolicyParameters",
		"metadata": map[string]any{
			"name":      ownerPolicy.Name,
			"namespace": ownerPolicy.Namespace,
			"labels":    ownerPolicy.Labels,
		},
		"spec": parameterSpec,
	}
	variableValues := map[string]any{}
	activation := map[string]any{
		"object": object, "oldObject": oldObject, "params": params,
		"request": map[string]any{"operation": func() string {
			if oldObject != nil {
				return "UPDATE"
			}
			return "CREATE"
		}(), "userInfo": map[string]any{
			"username": actor,
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
			return false, fmt.Errorf("policy expression %s: %w", expression, err)
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

// newPolicyEnvironment declares the five dynamic activation roots and CEL string
// extensions for test evaluation; dynamic types deliberately do not reproduce the
// API server's full resource schema or runtime admission configuration.
// newPolicyEnvironment объявляет пять dynamic-корней activation и CEL string
// extensions для тестов; dynamic types намеренно не воспроизводят полную schema
// ресурсов API server и runtime admission configuration.
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

package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestAgentMemoryGuardShellValidationFailsClosed(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	tests := []struct {
		name      string
		overrides []string
		wantError bool
	}{
		{name: "безопасный бюджет"},
		{name: "allocatable задан в Ki", overrides: []string{"MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY=131861544Ki"}},
		{name: "guard выключен", overrides: []string{"MATTERCODEX_RUNTIME_LIMITS_ENABLED=false"}, wantError: true},
		{name: "aggregate budget отсутствует", overrides: []string{"MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET="}, wantError: true},
		{name: "aggregate invariant нарушен", overrides: []string{"MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET=81Gi"}, wantError: true},
		{name: "scheduler request ниже limit", overrides: []string{"MATTERCODEX_AGENT_SESSION_MEMORY_REQUEST=4Gi"}, wantError: true},
	}
	baseEnvironment := []string{
		"MATTERCODEX_RUNTIME_ENABLED=true",
		"MATTERCODEX_RUNTIME_LIMITS_ENABLED=true",
		"MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY=120Gi",
		"MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET=80Gi",
		"MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE=40Gi",
		"MATTERCODEX_AGENT_SESSION_MEMORY_REQUEST=8Gi",
		"MATTERCODEX_AGENT_SESSION_MEMORY_LIMIT=8Gi",
		"MATTERCODEX_AGENT_UTILITY_MEMORY_LIMIT=4Gi",
		"MATTERCODEX_AGENT_DEV_SHM_SIZE_LIMIT=2Gi",
		"MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS=matter-codex-agent-workload",
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command("bash", "-c", `. "$1"; mattercodex_validate_agent_memory_guard`, "bash", filepath.Join(repositoryRoot, "scripts/lib/env.sh"))
			command.Env = append(os.Environ(), append(baseEnvironment, test.overrides...)...)
			output, err := command.CombinedOutput()
			if (err != nil) != test.wantError {
				t.Fatalf("validation error = %v, wantError=%t; output=%s", err, test.wantError, output)
			}
		})
	}
}

func TestRenderedAgentMemoryGuardAggregateAdmission(t *testing.T) {
	objects := renderAgentMemoryGuardObjects(t)
	agentQuota := objects["ResourceQuota/matter-codex-agent-memory-quota"]
	priorityClass := objects["PriorityClass/matter-codex-agent-workload"]
	configMap := objects["ConfigMap/matter-codex-bot-service-config"]
	if agentQuota == nil || priorityClass == nil || configMap == nil {
		t.Fatalf("runtime memory guard отрендерен не полностью: quota=%t priorityClass=%t configMap=%t", agentQuota != nil, priorityClass != nil, configMap != nil)
	}

	agentHard := renderedNestedMap(t, renderedNestedMap(t, agentQuota, "spec"), "hard")
	assertNestedString(t, agentHard, "requests.memory", "80Gi")
	assertNestedString(t, agentHard, "limits.memory", "80Gi")
	assertAgentQuotaScope(t, agentQuota, "matter-codex-agent-workload")
	if priorityClass["preemptionPolicy"] != "Never" || priorityClass["globalDefault"] != false {
		t.Fatalf("PriorityClass не изолирует admission без preemption: %#v", priorityClass)
	}

	config := renderedNestedMap(t, configMap, "data")
	assertNestedString(t, config, "MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY", "120Gi")
	assertNestedString(t, config, "MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET", "80Gi")
	assertNestedString(t, config, "MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE", "40Gi")
	assertRenderedAggregateMemoryInvariant(t, config)
	assertMultipleSessionPodAdmission(t, config, agentHard)
}

func renderAgentMemoryGuardObjects(t *testing.T) map[string]map[string]any {
	t.Helper()
	repositoryRoot := testRepositoryRoot(t)
	temporaryDirectory := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("test executable: %v", err)
	}
	envsubstPath := filepath.Join(temporaryDirectory, "envsubst")
	wrapper := "#!/usr/bin/env bash\nexec " + shellSingleQuote(executable) + " -test.run=TestEnvsubstHelperProcess --\n"
	if err := os.WriteFile(envsubstPath, []byte(wrapper), 0o700); err != nil {
		t.Fatalf("envsubst helper: %v", err)
	}
	envFile := filepath.Join(temporaryDirectory, "synthetic.env")
	envPayload := strings.Join([]string{
		"TARGET_HOST=synthetic.invalid",
		"TARGET_PORT=22",
		"TARGET_ROOT_USER=synthetic",
		"TARGET_ROOT_SSH_KEY=/tmp/synthetic-key",
		"OPERATOR_USER=synthetic",
		"OPERATOR_SSH_PUBKEY_PATH=/tmp/synthetic-pubkey",
		"PRODUCTION_NAMESPACE=mattermost",
		"PRODUCTION_DOMAIN=synthetic.invalid",
		"PUBLIC_BASE_URL=https://mattermost.synthetic.invalid",
		"LETSENCRYPT_EMAIL=synthetic@example.invalid",
		"MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY=120Gi",
		"MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET=80Gi",
		"MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE=40Gi",
	}, "\n") + "\n"
	if err := os.WriteFile(envFile, []byte(envPayload), 0o600); err != nil {
		t.Fatalf("synthetic env: %v", err)
	}
	renderDirectory := filepath.Join(temporaryDirectory, "render")
	render := exec.Command("bash", filepath.Join(repositoryRoot, "scripts/k8s/render-bot-service.sh"), "--env-file", envFile, "--render-dir", renderDirectory)
	render.Env = append(os.Environ(),
		"MATTERCODEX_TEST_ENVSUBST_HELPER=1",
		"PATH="+temporaryDirectory+":"+os.Getenv("PATH"),
	)
	if output, err := render.CombinedOutput(); err != nil {
		t.Fatalf("synthetic render: %v; output=%s", err, output)
	}

	objects := make(map[string]map[string]any)
	for _, yamlFile := range []string{"10-configmap.yaml", "15-runtime-limits.yaml"} {
		file, err := os.Open(filepath.Join(renderDirectory, yamlFile))
		if err != nil {
			t.Fatalf("открытие %s: %v", yamlFile, err)
		}
		decoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
		for {
			var object map[string]any
			if err := decoder.Decode(&object); err == io.EOF {
				break
			} else if err != nil {
				_ = file.Close()
				t.Fatalf("разбор %s: %v", yamlFile, err)
			}
			if len(object) == 0 {
				continue
			}
			metadata := renderedNestedMap(t, object, "metadata")
			kind, kindOK := object["kind"].(string)
			name, nameOK := metadata["name"].(string)
			if !kindOK || !nameOK {
				_ = file.Close()
				t.Fatalf("%s содержит объект без kind/metadata.name", yamlFile)
			}
			objects[kind+"/"+name] = object
		}
		if err := file.Close(); err != nil {
			t.Fatalf("закрытие %s: %v", yamlFile, err)
		}
	}
	return objects
}

func assertAgentQuotaScope(t *testing.T, quota map[string]any, expectedPriorityClass string) {
	t.Helper()
	scopeSelector := renderedNestedMap(t, renderedNestedMap(t, quota, "spec"), "scopeSelector")
	expressions, ok := scopeSelector["matchExpressions"].([]any)
	if !ok || len(expressions) != 1 {
		t.Fatalf("ResourceQuota scopeSelector.matchExpressions = %#v", scopeSelector["matchExpressions"])
	}
	expression, ok := expressions[0].(map[string]any)
	if !ok || expression["scopeName"] != "PriorityClass" || expression["operator"] != "In" {
		t.Fatalf("ResourceQuota scope expression = %#v", expressions[0])
	}
	values, ok := expression["values"].([]any)
	if !ok || len(values) != 1 || values[0] != expectedPriorityClass {
		t.Fatalf("ResourceQuota scope values = %#v", expression["values"])
	}
}

func assertRenderedAggregateMemoryInvariant(t *testing.T, config map[string]any) {
	t.Helper()
	nodeMemory := renderedMemoryQuantity(t, config, "MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY")
	agentBudget := renderedMemoryQuantity(t, config, "MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET")
	systemReserve := renderedMemoryQuantity(t, config, "MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE")
	total := agentBudget.DeepCopy()
	total.Add(systemReserve)
	if total.Cmp(nodeMemory) > 0 {
		t.Fatalf("aggregate invariant нарушен: agent=%s reserve=%s node=%s", agentBudget.String(), systemReserve.String(), nodeMemory.String())
	}
}

func assertMultipleSessionPodAdmission(t *testing.T, config map[string]any, quotaHard map[string]any) {
	t.Helper()
	priorityClass := config["MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS"].(string)
	sessionMemory := config["MATTERCODEX_AGENT_SESSION_MEMORY_LIMIT"].(string)
	utilityMemory := config["MATTERCODEX_AGENT_UTILITY_MEMORY_LIMIT"].(string)
	budget := renderedMemoryQuantity(t, quotaHard, "limits.memory")

	sessionPods := make([]corev1.Pod, 0, 10)
	for index := 0; index < 9; index++ {
		sessionPods = append(sessionPods, renderedAdmissionPod(fmt.Sprintf("session-%d", index), priorityClass, sessionMemory))
	}
	tenthSession := renderedAdmissionPod("session-9", priorityClass, sessionMemory)
	if !scopedMemoryQuotaAdmits(sessionPods, tenthSession, priorityClass, budget) {
		t.Fatal("aggregate admission отклонил десятый session pod, который заполняет бюджет ровно")
	}
	withUtility := append(append([]corev1.Pod(nil), sessionPods...), renderedAdmissionPod("utility", priorityClass, utilityMemory))
	if scopedMemoryQuotaAdmits(withUtility, tenthSession, priorityClass, budget) {
		t.Fatal("aggregate admission разрешил session pod, который превысил бы memory budget")
	}
}

func renderedAdmissionPod(name string, priorityClass string, memory string) corev1.Pod {
	quantity := resource.MustParse(memory)
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PodSpec{
			PriorityClassName: priorityClass,
			Containers: []corev1.Container{{
				Name: "runner",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceMemory: quantity},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: quantity},
				},
			}},
		},
	}
}

func scopedMemoryQuotaAdmits(existing []corev1.Pod, candidate corev1.Pod, priorityClass string, budget resource.Quantity) bool {
	totalRequests := resource.MustParse("0")
	totalLimits := resource.MustParse("0")
	for _, pod := range append(append([]corev1.Pod(nil), existing...), candidate) {
		if pod.Spec.PriorityClassName != priorityClass {
			continue
		}
		for _, container := range pod.Spec.Containers {
			request := container.Resources.Requests[corev1.ResourceMemory]
			limit := container.Resources.Limits[corev1.ResourceMemory]
			totalRequests.Add(request)
			totalLimits.Add(limit)
		}
	}
	return totalRequests.Cmp(budget) <= 0 && totalLimits.Cmp(budget) <= 0
}

func renderedMemoryQuantity(t *testing.T, values map[string]any, key string) resource.Quantity {
	t.Helper()
	value, ok := values[key].(string)
	if !ok {
		t.Fatalf("поле %s = %#v, ожидалась строка", key, values[key])
	}
	quantity, err := resource.ParseQuantity(value)
	if err != nil {
		t.Fatalf("поле %s = %q: %v", key, value, err)
	}
	return quantity
}

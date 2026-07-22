package app

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
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
		{
			name: "верхняя поддерживаемая граница",
			overrides: []string{
				"MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY=8388607Ti",
				"MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET=8388606Ti",
				"MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE=1Ti",
			},
		},
		{name: "quantity выше единого предела", overrides: []string{"MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY=8388608Ti"}, wantError: true},
		{name: "quantity не переполняет shell", overrides: []string{"MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY=999999999Ti"}, wantError: true},
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

func TestAgentMemoryGuardQuantityRangeRenderParity(t *testing.T) {
	tests := []struct {
		name      string
		node      string
		budget    string
		reserve   string
		wantError bool
	}{
		{name: "верхняя поддерживаемая граница", node: "8388607Ti", budget: "8388606Ti", reserve: "1Ti"},
		{name: "выше единого предела", node: "8388608Ti", budget: "80Gi", reserve: "40Gi", wantError: true},
		{name: "переполнение запрещено одинаково", node: "999999999Ti", budget: "80Gi", reserve: "40Gi", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, err := runSyntheticAgentMemoryGuardRender(t, test.node, test.budget, test.reserve)
			if (err != nil) != test.wantError {
				t.Fatalf("render error = %v, wantError=%t; output=%s", err, test.wantError, output)
			}
		})
	}
}

func TestAgentWorkloadInventoryFailsClosedForLegacyPods(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	guardedPod := func(priorityClass string, serviceAccount string, request string, limit string, managed bool) map[string]any {
		labels := map[string]any{}
		if managed {
			labels["app.kubernetes.io/name"] = "matter-codex-agent-runner"
		}
		return map[string]any{
			"metadata": map[string]any{"name": "synthetic-agent-pod", "labels": labels},
			"spec": map[string]any{
				"priorityClassName":  priorityClass,
				"serviceAccountName": serviceAccount,
				"containers": []any{map[string]any{
					"name": "runner",
					"resources": map[string]any{
						"requests": map[string]any{"memory": request},
						"limits":   map[string]any{"memory": limit},
					},
				}},
			},
		}
	}
	tests := []struct {
		name      string
		items     []any
		wantError bool
	}{
		{name: "пустой inventory", items: []any{}},
		{name: "guarded session", items: []any{guardedPod("matter-codex-agent-workload", "matter-codex-agent-runner", "8Gi", "8Gi", true)}},
		{name: "guarded utility", items: []any{guardedPod("matter-codex-agent-workload", "matter-codex-agent-runner", "4Gi", "4Gi", true)}},
		{name: "legacy priority class", items: []any{guardedPod("legacy-agent-workload", "matter-codex-agent-runner", "8Gi", "8Gi", true)}, wantError: true},
		{name: "legacy request limit", items: []any{guardedPod("matter-codex-agent-workload", "matter-codex-agent-runner", "1Gi", "64Gi", true)}, wantError: true},
		{name: "cluster admin labeled", items: []any{guardedPod("matter-codex-agent-workload", "matter-codex-agent-runner-cluster-admin", "8Gi", "8Gi", true)}, wantError: true},
		{name: "cluster admin unlabeled", items: []any{guardedPod("", "matter-codex-agent-runner-cluster-admin", "1Gi", "1Gi", false)}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{"items": test.items})
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			command := exec.Command("bash", "-c", `. "$1"; mattercodex_validate_agent_workload_inventory`, "bash", filepath.Join(repositoryRoot, "scripts/lib/env.sh"))
			command.Env = append(os.Environ(),
				"MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET=80Gi",
				"MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS=matter-codex-agent-workload",
				"MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT=matter-codex-agent-runner",
				"MATTERCODEX_AGENT_RUNNER_CLUSTER_ADMIN_SERVICE_ACCOUNT=matter-codex-agent-runner-cluster-admin",
			)
			command.Stdin = strings.NewReader(string(payload))
			output, err := command.CombinedOutput()
			if (err != nil) != test.wantError {
				t.Fatalf("inventory error = %v, wantError=%t; output=%s", err, test.wantError, output)
			}
		})
	}
}

func runSyntheticAgentMemoryGuardRender(t *testing.T, node string, budget string, reserve string) ([]byte, error) {
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
		"MATTERCODEX_RUNTIME_NODE_ALLOCATABLE_MEMORY=" + node,
		"MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET=" + budget,
		"MATTERCODEX_RUNTIME_SYSTEM_MEMORY_RESERVE=" + reserve,
	}, "\n") + "\n"
	if err := os.WriteFile(envFile, []byte(envPayload), 0o600); err != nil {
		t.Fatalf("synthetic env: %v", err)
	}
	render := exec.Command("bash", filepath.Join(repositoryRoot, "scripts/k8s/render-bot-service.sh"), "--env-file", envFile, "--render-dir", filepath.Join(temporaryDirectory, "render"))
	render.Env = append(os.Environ(),
		"MATTERCODEX_TEST_ENVSUBST_HELPER=1",
		"PATH="+temporaryDirectory+":"+os.Getenv("PATH"),
	)
	output, err := render.CombinedOutput()
	return output, err
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

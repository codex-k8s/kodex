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

func TestAgentWorkloadInventoryFailsClosedForUnknownPods(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	labels := func(name string) map[string]any {
		return map[string]any{"app.kubernetes.io/name": name}
	}
	ownerReference := func(kind string, name string, uid string) map[string]any {
		return map[string]any{
			"apiVersion":         "apps/v1",
			"kind":               kind,
			"name":               name,
			"uid":                uid,
			"controller":         true,
			"blockOwnerDeletion": true,
		}
	}
	metadata := func(name string, uid string, objectLabels map[string]any) map[string]any {
		return map[string]any{
			"name":            name,
			"uid":             uid,
			"resourceVersion": "1",
			"labels":          objectLabels,
		}
	}
	container := func(name string, image string) map[string]any {
		return map[string]any{
			"name":            name,
			"image":           image,
			"imagePullPolicy": "IfNotPresent",
		}
	}
	podSpec := func(serviceAccount string, containerName string, image string) map[string]any {
		return map[string]any{
			"serviceAccountName": serviceAccount,
			"containers":         []any{container(containerName, image)},
		}
	}
	deploymentPodInventory := func(name string, serviceAccount string, containerName string, image string) []any {
		deploymentUID := "uid-deployment-" + name
		replicaSetName := name + "-7d9f6d8f5"
		replicaSetUID := "uid-replicaset-" + name
		deploymentMetadata := metadata(name, deploymentUID, labels(name))
		replicaSetMetadata := metadata(replicaSetName, replicaSetUID, labels(name))
		replicaSetMetadata["ownerReferences"] = []any{ownerReference("Deployment", name, deploymentUID)}
		podMetadata := metadata(replicaSetName+"-abcde", "uid-pod-"+name, labels(name))
		podMetadata["generateName"] = replicaSetName + "-"
		podMetadata["ownerReferences"] = []any{ownerReference("ReplicaSet", replicaSetName, replicaSetUID)}
		selector := map[string]any{"matchLabels": labels(name)}
		template := map[string]any{
			"metadata": map[string]any{"labels": labels(name)},
			"spec":     podSpec(serviceAccount, containerName, image),
		}
		return []any{
			map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata":   deploymentMetadata,
				"spec": map[string]any{
					"selector": selector,
					"template": template,
				},
			},
			map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "ReplicaSet",
				"metadata":   replicaSetMetadata,
				"spec": map[string]any{
					"selector": selector,
					"template": template,
				},
			},
			map[string]any{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata":   podMetadata,
				"spec":       podSpec(serviceAccount, containerName, image),
			},
		}
	}
	postgresInventory := func() []any {
		statefulSetUID := "uid-statefulset-mattermost-postgres"
		statefulSetMetadata := metadata("mattermost-postgres", statefulSetUID, labels("mattermost-postgres"))
		statefulSetContainer := container("postgres", "postgres:16")
		statefulSetContainer["volumeMounts"] = []any{
			map[string]any{"name": "postgres-data", "mountPath": "/var/lib/postgresql/data"},
		}
		templateSpec := map[string]any{
			"containers": []any{statefulSetContainer},
		}
		podMetadata := metadata("mattermost-postgres-0", "uid-pod-mattermost-postgres", labels("mattermost-postgres"))
		podMetadata["ownerReferences"] = []any{ownerReference("StatefulSet", "mattermost-postgres", statefulSetUID)}
		podContainer := container("postgres", "postgres:16")
		podContainer["volumeMounts"] = []any{
			map[string]any{"name": "postgres-data", "mountPath": "/var/lib/postgresql/data"},
		}
		return []any{
			map[string]any{
				"apiVersion": "apps/v1",
				"kind":       "StatefulSet",
				"metadata":   statefulSetMetadata,
				"spec": map[string]any{
					"selector": map[string]any{"matchLabels": labels("mattermost-postgres")},
					"template": map[string]any{
						"metadata": map[string]any{"labels": labels("mattermost-postgres")},
						"spec":     templateSpec,
					},
					"volumeClaimTemplates": []any{
						map[string]any{"metadata": map[string]any{"name": "postgres-data"}},
					},
				},
			},
			map[string]any{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata":   podMetadata,
				"spec": map[string]any{
					"containers": []any{podContainer},
					"volumes": []any{
						map[string]any{
							"name": "postgres-data",
							"persistentVolumeClaim": map[string]any{
								"claimName": "postgres-data-mattermost-postgres-0",
							},
						},
					},
				},
			},
		}
	}
	unknownPod := func(name string, podLabels map[string]any, serviceAccount string, priorityClass string, request string, limit string) map[string]any {
		return map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata":   metadata(name, "uid-pod-"+name, podLabels),
			"spec": map[string]any{
				"priorityClassName":  priorityClass,
				"serviceAccountName": serviceAccount,
				"containers": []any{map[string]any{
					"name":  "runner",
					"image": "synthetic.invalid/runner:test",
					"resources": map[string]any{
						"requests": map[string]any{"memory": request},
						"limits":   map[string]any{"memory": limit},
					},
				}},
			},
		}
	}
	mattermostInventory := deploymentPodInventory("mattermost", "mattermost", "mattermost", "mattermost/mattermost-team-edition:latest")
	botServiceInventory := deploymentPodInventory("matter-codex-bot-service", "matter-codex-bot-service", "bot-service", "synthetic.invalid/bot-service:test")
	oauthProxyInventory := deploymentPodInventory("mattermost-oauth2-proxy", "mattermost-oauth2-proxy", "oauth2-proxy", "quay.io/oauth2-proxy/oauth2-proxy:v7")
	registryInventory := deploymentPodInventory("matter-codex-registry", "default", "registry", "registry:2")
	mattermostPod := mattermostInventory[len(mattermostInventory)-1].(map[string]any)
	mattermostContainer := mattermostPod["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
	mattermostContainer["resources"] = map[string]any{
		"requests": map[string]any{"memory": "1Gi"},
		"limits":   map[string]any{"memory": "64Gi"},
	}
	tests := []struct {
		name      string
		items     []any
		wantError bool
	}{
		{name: "пустой inventory", items: []any{}},
		{name: "Mattermost из доверенного Deployment", items: mattermostInventory},
		{name: "bot-service из доверенного Deployment", items: botServiceInventory},
		{name: "PostgreSQL из доверенного StatefulSet", items: postgresInventory()},
		{name: "OAuth2 proxy из доверенного Deployment", items: oauthProxyInventory},
		{name: "registry из доверенного Deployment", items: registryInventory},
		{
			name: "точный residual repro без label с ordinary ServiceAccount",
			items: []any{
				unknownPod("synthetic-unknown-pod", map[string]any{}, "ordinary-service-account", "", "1Gi", "64Gi"),
			},
			wantError: true,
		},
		{
			name: "residual repro с неверным PriorityClass",
			items: []any{
				unknownPod("synthetic-wrong-priority-pod", map[string]any{}, "default", "legacy-agent-workload", "1Gi", "64Gi"),
			},
			wantError: true,
		},
		{
			name: "agent label и ServiceAccount не являются доказательством ownership",
			items: []any{
				unknownPod("synthetic-agent-spoof-pod", labels("matter-codex-agent-runner"), "matter-codex-agent-runner", "matter-codex-agent-workload", "8Gi", "8Gi"),
			},
			wantError: true,
		},
		{
			name: "platform label и ServiceAccount не являются доказательством ownership",
			items: []any{
				unknownPod("synthetic-platform-spoof-pod", labels("mattermost"), "mattermost", "", "1Gi", "64Gi"),
			},
			wantError: true,
		},
		{
			name:      "неизвестный Deployment не входит в allowlist",
			items:     deploymentPodInventory("unknown-platform", "default", "unknown", "synthetic.invalid/unknown:test"),
			wantError: true,
		},
		{
			name: "заявленный ReplicaSet отсутствует в inventory",
			items: []any{
				deploymentPodInventory("mattermost", "mattermost", "mattermost", "mattermost/mattermost-team-edition:latest")[2],
			},
			wantError: true,
		},
		{
			name: "Pod не соответствует шаблону доверенного ReplicaSet",
			items: func() []any {
				items := deploymentPodInventory("mattermost", "mattermost", "mattermost", "mattermost/mattermost-team-edition:latest")
				pod := items[2].(map[string]any)
				pod["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)["image"] = "synthetic.invalid/unknown:test"
				return items
			}(),
			wantError: true,
		},
		{
			name: "legacy cluster admin Pod",
			items: []any{
				unknownPod("synthetic-cluster-admin-pod", labels("matter-codex-agent-runner"), "matter-codex-agent-runner-cluster-admin", "", "1Gi", "1Gi"),
			},
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{
				"apiVersion": "v1",
				"kind":       "List",
				"items":      test.items,
			})
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			command := exec.Command("bash", "-c", `. "$1"; mattercodex_validate_agent_workload_inventory`, "bash", filepath.Join(repositoryRoot, "scripts/lib/env.sh"))
			command.Env = append(os.Environ(),
				"MATTERCODEX_RUNTIME_AGENT_MEMORY_BUDGET=80Gi",
				"MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS=matter-codex-agent-workload",
				"MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT=matter-codex-agent-runner",
				"MATTERCODEX_AGENT_RUNNER_CLUSTER_ADMIN_SERVICE_ACCOUNT=matter-codex-agent-runner-cluster-admin",
				"MATTERCODEX_IMAGE_REGISTRY_NAME=matter-codex-registry",
			)
			command.Stdin = strings.NewReader(string(payload))
			output, err := command.CombinedOutput()
			if (err != nil) != test.wantError {
				t.Fatalf("inventory error = %v, wantError=%t; output=%s", err, test.wantError, output)
			}
		})
	}
}

func TestBotServiceInstallersValidateInventoryBeforeKubernetesMutation(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	for _, relativePath := range []string{
		"scripts/k8s/install-bot-service.sh",
		"scripts/remote/install-bot-service.sh",
	} {
		t.Run(relativePath, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(repositoryRoot, relativePath))
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			script := string(content)
			inventoryIndex := strings.Index(script, "mattercodex_validate_agent_workload_inventory")
			clusterRoleBindingDeleteIndex := strings.Index(script, "delete clusterrolebinding matter-codex-agent-runner-cluster-admin")
			serviceAccountDeleteIndex := strings.Index(script, `delete serviceaccount`)
			if inventoryIndex < 0 || clusterRoleBindingDeleteIndex < 0 || serviceAccountDeleteIndex < 0 {
				t.Fatalf("installer не содержит полный переходный guard: inventory=%d clusterRoleBindingDelete=%d serviceAccountDelete=%d", inventoryIndex, clusterRoleBindingDeleteIndex, serviceAccountDeleteIndex)
			}
			if inventoryIndex > clusterRoleBindingDeleteIndex || inventoryIndex > serviceAccountDeleteIndex {
				t.Fatalf("инвентаризация выполняется после Kubernetes mutation: inventory=%d clusterRoleBindingDelete=%d serviceAccountDelete=%d", inventoryIndex, clusterRoleBindingDeleteIndex, serviceAccountDeleteIndex)
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

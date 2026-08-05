package app

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestEnvsubstHelperProcess(t *testing.T) {
	if os.Getenv("MATTERCODEX_TEST_ENVSUBST_HELPER") != "1" {
		return
	}
	payload, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	_, _ = os.Stdout.WriteString(os.ExpandEnv(string(payload)))
	os.Exit(0)
}

func TestBotServiceRenderCountsNonEmptyObjects(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	temporaryDirectory := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("test executable: %v", err)
	}
	envsubstPath := filepath.Join(temporaryDirectory, "envsubst")
	wrapper := "#!/bin/bash\nexec " + shellSingleQuote(executable) + " -test.run=TestEnvsubstHelperProcess --\n"
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
	}, "\n") + "\n"
	if err := os.WriteFile(envFile, []byte(envPayload), 0o600); err != nil {
		t.Fatalf("synthetic env: %v", err)
	}
	renderDirectory := filepath.Join(temporaryDirectory, "render")
	render := exec.Command(filepath.Join(repositoryRoot, "scripts/k8s/render-bot-service.sh"), "--env-file", envFile, "--render-dir", renderDirectory)
	render.Env = append(os.Environ(),
		"MATTERCODEX_TEST_ENVSUBST_HELPER=1",
		"PATH="+temporaryDirectory+":"+os.Getenv("PATH"),
	)
	if output, err := render.CombinedOutput(); err != nil {
		t.Fatalf("synthetic render: %v; output=%s", err, output)
	}
	yamlFiles, err := filepath.Glob(filepath.Join(renderDirectory, "*.yaml"))
	if err != nil {
		t.Fatalf("поиск отрендеренных YAML: %v", err)
	}
	if len(yamlFiles) != 9 {
		t.Fatalf("число YAML-файлов = %d, ожидалось 9", len(yamlFiles))
	}
	objectCount := 0
	var runtimeQuota map[string]any
	var runtimeLimitRange map[string]any
	for _, yamlFile := range yamlFiles {
		file, err := os.Open(yamlFile)
		if err != nil {
			t.Fatalf("открытие %s: %v", filepath.Base(yamlFile), err)
		}
		decoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
		for {
			var object map[string]any
			err := decoder.Decode(&object)
			if err == io.EOF {
				break
			}
			if err != nil {
				_ = file.Close()
				t.Fatalf("разбор %s: %v", filepath.Base(yamlFile), err)
			}
			if len(object) == 0 {
				continue
			}
			kind, kindOK := object["kind"].(string)
			metadata, metadataOK := object["metadata"].(map[string]any)
			name, nameOK := metadata["name"].(string)
			if !kindOK || strings.TrimSpace(kind) == "" || !metadataOK || !nameOK || strings.TrimSpace(name) == "" {
				_ = file.Close()
				t.Fatalf("%s содержит объект без kind/metadata.name", filepath.Base(yamlFile))
			}
			switch kind + "/" + name {
			case "ResourceQuota/matter-codex-runtime-quota":
				runtimeQuota = object
			case "LimitRange/matter-codex-runtime-container-defaults":
				runtimeLimitRange = object
			}
			objectCount++
		}
		if err := file.Close(); err != nil {
			t.Fatalf("закрытие %s: %v", filepath.Base(yamlFile), err)
		}
	}
	if objectCount != 20 {
		t.Fatalf("число Kubernetes objects = %d, ожидалось 20", objectCount)
	}
	assertRenderedRuntimeResourcePolicy(t, runtimeQuota, runtimeLimitRange)
}

func assertRenderedRuntimeResourcePolicy(t *testing.T, quota map[string]any, limitRange map[string]any) {
	t.Helper()
	if quota == nil || limitRange == nil {
		t.Fatalf("runtime ResourceQuota/LimitRange не найдены: quota=%t limitRange=%t", quota != nil, limitRange != nil)
	}
	hard := renderedNestedMap(t, renderedNestedMap(t, quota, "spec"), "hard")
	if _, exists := hard["limits.memory"]; exists {
		t.Fatalf("ResourceQuota не должна содержать aggregate limits.memory: %#v", hard)
	}
	assertNestedString(t, hard, "requests.cpu", "28")
	assertNestedString(t, hard, "requests.memory", "96Gi")

	limits, ok := renderedNestedMap(t, limitRange, "spec")["limits"].([]any)
	if !ok || len(limits) != 1 {
		t.Fatalf("LimitRange spec.limits = %#v", renderedNestedMap(t, limitRange, "spec")["limits"])
	}
	containerDefaults, ok := limits[0].(map[string]any)
	if !ok {
		t.Fatalf("LimitRange container defaults = %#v", limits[0])
	}
	if _, exists := containerDefaults["default"]; exists {
		t.Fatalf("LimitRange не должна добавлять default limits: %#v", containerDefaults)
	}
	requests := renderedNestedMap(t, containerDefaults, "defaultRequest")
	assertNestedString(t, requests, "cpu", "500m")
	assertNestedString(t, requests, "memory", "1Gi")
}

func renderedNestedMap(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := object[key].(map[string]any)
	if !ok {
		t.Fatalf("поле %s = %#v, ожидался object", key, object[key])
	}
	return value
}

func assertNestedString(t *testing.T, object map[string]any, key string, expected string) {
	t.Helper()
	if got, ok := object[key].(string); !ok || got != expected {
		t.Fatalf("поле %s = %#v, ожидалось %q", key, object[key], expected)
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

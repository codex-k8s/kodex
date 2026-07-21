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
	if _, err := exec.LookPath("bash"); err != nil {
		t.Fatal("для render regression обязательна команда bash")
	}
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
	yamlFiles, err := filepath.Glob(filepath.Join(renderDirectory, "*.yaml"))
	if err != nil {
		t.Fatalf("поиск отрендеренных YAML: %v", err)
	}
	if len(yamlFiles) != 8 {
		t.Fatalf("число YAML-файлов = %d, ожидалось 8", len(yamlFiles))
	}
	objectCount := 0
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
			objectCount++
		}
		if err := file.Close(); err != nil {
			t.Fatalf("закрытие %s: %v", filepath.Base(yamlFile), err)
		}
	}
	if objectCount != 18 {
		t.Fatalf("число Kubernetes objects = %d, ожидалось 18", objectCount)
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

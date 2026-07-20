package app

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	for _, commandName := range []string{"bash", "kubectl", "yq"} {
		if _, err := exec.LookPath(commandName); err != nil {
			t.Fatalf("для render regression обязательна команда %s", commandName)
		}
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
	verify := exec.Command("bash", filepath.Join(repositoryRoot, "scripts/k8s/verify-rendered-objects.sh"),
		"--render-dir", renderDirectory, "--expected-files", "8", "--expected-objects", "18")
	output, err := verify.CombinedOutput()
	if err != nil {
		t.Fatalf("render evidence: %v; output=%s", err, output)
	}
	if !strings.Contains(string(output), "files=8 objects=18 client_dry_run=18") {
		t.Fatalf("render evidence output = %s", output)
	}
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestBotServicePodInputRevisionIsStableAndSensitiveToEveryInput(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	revision := func(inputs ...string) string {
		t.Helper()
		arguments := []string{"-c", `. "$1"; shift; mattercodex_pod_input_revision "$@"`, "revision", filepath.Join(repositoryRoot, "scripts/lib/env.sh")}
		arguments = append(arguments, inputs...)
		command := exec.Command("bash", arguments...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("mattercodex_pod_input_revision() error = %v; output=%s", err, output)
		}
		return strings.TrimSpace(string(output))
	}
	base := []string{
		"configmap/config:uid-config:10",
		"secret/bot:uid-bot:20",
		"secret/postgres:uid-postgres:30",
		"secret/github:uid-github:40",
		"secret/artifact-storage:uid-artifact-storage:50",
	}
	first := revision(base...)
	if second := revision(base...); second != first {
		t.Fatalf("unchanged inputs changed revision: %q != %q", second, first)
	}
	for index := range base {
		changed := append([]string(nil), base...)
		changed[index] += "-rotated"
		if got := revision(changed...); got == first {
			t.Fatalf("input %d did not change revision", index)
		}
	}
}

func TestBotServicePodTemplateRollsOncePerRevisionOrImageChange(t *testing.T) {
	base := renderedBotServicePodTemplate(t, "revision-a", "registry.invalid/bot-service:a")
	unchanged := renderedBotServicePodTemplate(t, "revision-a", "registry.invalid/bot-service:a")
	if !reflect.DeepEqual(base, unchanged) {
		t.Fatal("unchanged pod inputs changed the pod template")
	}

	rotatedSecret := renderedBotServicePodTemplate(t, "revision-b", "registry.invalid/bot-service:a")
	if reflect.DeepEqual(base, rotatedSecret) {
		t.Fatal("pod input revision change did not change the pod template")
	}
	normalizeBotServicePodRevision(base)
	normalizeBotServicePodRevision(rotatedSecret)
	if !reflect.DeepEqual(base, rotatedSecret) {
		t.Fatal("pod input revision changed more than the single revision annotation")
	}

	base = renderedBotServicePodTemplate(t, "revision-a", "registry.invalid/bot-service:a")
	changedImage := renderedBotServicePodTemplate(t, "revision-a", "registry.invalid/bot-service:b")
	if reflect.DeepEqual(base, changedImage) {
		t.Fatal("image change did not change the pod template")
	}
	normalizeBotServiceImage(base)
	normalizeBotServiceImage(changedImage)
	if !reflect.DeepEqual(base, changedImage) {
		t.Fatal("image change changed more than the single container image field")
	}
}

func TestBotServiceInstallersRevisionAllReferencedPodInputs(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	for _, relativePath := range []string{
		"scripts/k8s/install-bot-service.sh",
		"scripts/remote/install-bot-service.sh",
	} {
		payload, err := os.ReadFile(filepath.Join(repositoryRoot, relativePath))
		if err != nil {
			t.Fatalf("read %s: %v", relativePath, err)
		}
		body := string(payload)
		for _, name := range []string{
			"MATTERCODEX_BOT_SERVICE_CONFIG_CONFIGMAP",
			"MATTERCODEX_BOT_SERVICE_SECRET",
			"MATTERCODEX_POSTGRES_SECRET",
			"MATTERCODEX_GITHUB_SECRET",
			"MATTERCODEX_ARTIFACT_STORAGE_SECRET",
		} {
			if !strings.Contains(body, name) {
				t.Errorf("%s does not include %s in pod input revision", relativePath, name)
			}
		}
		if strings.Contains(body, "rollout restart") {
			t.Errorf("%s contains an unconditional duplicate rollout", relativePath)
		}
	}
}

func renderedBotServicePodTemplate(t *testing.T, revision string, image string) map[string]any {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(testRepositoryRoot(t), "deploy/k8s/bot-service/deployment.yaml.tpl"))
	if err != nil {
		t.Fatalf("read deployment template: %v", err)
	}
	body := strings.ReplaceAll(string(payload), "${MATTERCODEX_BOT_SERVICE_POD_INPUT_REVISION}", revision)
	body = strings.ReplaceAll(body, "${MATTERCODEX_BOT_SERVICE_IMAGE}", image)
	decoder := k8syaml.NewYAMLOrJSONDecoder(bytes.NewBufferString(body), 4096)
	var deployment map[string]any
	if err := decoder.Decode(&deployment); err != nil {
		t.Fatalf("decode deployment template: %v", err)
	}
	return nestedMap(deployment, "spec", "template")
}

func normalizeBotServicePodRevision(template map[string]any) {
	annotations := nestedMap(template, "metadata", "annotations")
	annotations["matter-codex.kodex.works/pod-input-revision"] = "normalized"
}

func normalizeBotServiceImage(template map[string]any) {
	containers := sliceValue(nestedMap(template, "spec")["containers"])
	for _, rawContainer := range containers {
		container, _ := rawContainer.(map[string]any)
		if stringValue(container["name"]) == "bot-service" {
			container["image"] = "normalized"
		}
	}
}

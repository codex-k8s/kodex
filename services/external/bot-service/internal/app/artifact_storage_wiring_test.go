package app

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestArtifactStorageSecretHasExactOpaqueContract(t *testing.T) {
	objects := decodeYAMLDocuments(t, filepath.Join(testRepositoryRoot(t), "deploy/k8s/bot-service/artifact-storage-secret.yaml.tpl"))
	if len(objects) != 1 {
		t.Fatalf("число объектов artifact storage Secret = %d", len(objects))
	}
	secret := objects[0]
	if stringValue(secret["kind"]) != "Secret" || stringValue(secret["type"]) != "Opaque" || nestedString(secret, "metadata", "name") != "${MATTERCODEX_ARTIFACT_STORAGE_SECRET}" {
		t.Fatalf("контракт artifact storage Secret = %#v", secret)
	}
	data := mapValue(secret["data"])
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	wantKeys := []string{"access-key-id", "bucket", "endpoint", "secret-access-key"}
	if !slices.Equal(keys, wantKeys) {
		t.Fatalf("data keys = %v, ожидались %v", keys, wantKeys)
	}
	wantValues := map[string]string{
		"access-key-id":     "${ARTIFACT_S3_ACCESS_KEY_ID_B64}",
		"secret-access-key": "${ARTIFACT_S3_SECRET_ACCESS_KEY_B64}",
		"bucket":            "${ARTIFACT_S3_BUCKET_B64}",
		"endpoint":          "${ARTIFACT_S3_ENDPOINT_B64}",
	}
	for key, value := range wantValues {
		if actual := stringValue(data[key]); actual != value {
			t.Errorf("Secret data[%s] = %q, ожидался %q", key, actual, value)
		}
	}
}

func TestArtifactStorageConfigMapContainsOnlyNonSecretSettings(t *testing.T) {
	objects := decodeYAMLDocuments(t, filepath.Join(testRepositoryRoot(t), "deploy/k8s/bot-service/configmap.yaml.tpl"))
	if len(objects) != 1 {
		t.Fatalf("число объектов ConfigMap = %d", len(objects))
	}
	data := mapValue(objects[0]["data"])
	for _, denied := range []string{
		"MATTERCODEX_ARTIFACT_S3_ACCESS_KEY_ID",
		"MATTERCODEX_ARTIFACT_S3_SECRET_ACCESS_KEY",
		"MATTERCODEX_ARTIFACT_S3_BUCKET",
		"MATTERCODEX_ARTIFACT_S3_ENDPOINT",
	} {
		if _, exists := data[denied]; exists {
			t.Errorf("ConfigMap содержит secret-owned ключ %s", denied)
		}
	}
	for _, allowed := range []string{
		"MATTERCODEX_ARTIFACTS_ENABLED",
		"MATTERCODEX_ARTIFACT_STORAGE_SECRET",
		"MATTERCODEX_ARTIFACT_S3_REGION",
		"MATTERCODEX_ARTIFACT_S3_USE_PATH_STYLE",
		"MATTERCODEX_ARTIFACT_MAX_FILES_PER_TURN",
		"MATTERCODEX_ARTIFACT_MAX_OBJECT_BYTES",
		"MATTERCODEX_ARTIFACT_MAX_TURN_BYTES",
	} {
		if _, exists := data[allowed]; !exists {
			t.Errorf("ConfigMap не содержит несекретную настройку %s", allowed)
		}
	}
}

func TestBotServiceReceivesExactlyFourArtifactStorageSecretKeyRefs(t *testing.T) {
	objects := decodeYAMLDocuments(t, filepath.Join(testRepositoryRoot(t), "deploy/k8s/bot-service/deployment.yaml.tpl"))
	containers := sliceValue(nestedMap(objects[0], "spec", "template", "spec")["containers"])
	if len(containers) != 1 {
		t.Fatalf("число containers = %d", len(containers))
	}
	container := mapValue(containers[0])
	actual := map[string]string{}
	for _, rawEnv := range sliceValue(container["env"]) {
		env := mapValue(rawEnv)
		name := stringValue(env["name"])
		if !strings.HasPrefix(name, "MATTERCODEX_ARTIFACT_S3_") {
			continue
		}
		ref := nestedMap(env, "valueFrom", "secretKeyRef")
		if stringValue(ref["name"]) != "${MATTERCODEX_ARTIFACT_STORAGE_SECRET}" || ref["optional"] != true {
			t.Fatalf("secretKeyRef %s = %#v", name, ref)
		}
		actual[name] = stringValue(ref["key"])
	}
	want := map[string]string{
		"MATTERCODEX_ARTIFACT_S3_ACCESS_KEY_ID":     "access-key-id",
		"MATTERCODEX_ARTIFACT_S3_SECRET_ACCESS_KEY": "secret-access-key",
		"MATTERCODEX_ARTIFACT_S3_BUCKET":            "bucket",
		"MATTERCODEX_ARTIFACT_S3_ENDPOINT":          "endpoint",
	}
	if len(actual) != len(want) {
		t.Fatalf("artifact secretKeyRef = %#v", actual)
	}
	for name, key := range want {
		if actual[name] != key {
			t.Errorf("secretKeyRef %s = %q, ожидался %q", name, actual[name], key)
		}
	}
}

func TestArtifactStorageInstallersRenderAllKeysWithoutTracing(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	for _, relativePath := range []string{"scripts/k8s/install-bot-service.sh", "scripts/remote/install-bot-service.sh"} {
		body, err := os.ReadFile(filepath.Join(repositoryRoot, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, variable := range []string{
			"ARTIFACT_S3_ACCESS_KEY_ID_B64",
			"ARTIFACT_S3_SECRET_ACCESS_KEY_B64",
			"ARTIFACT_S3_BUCKET_B64",
			"ARTIFACT_S3_ENDPOINT_B64",
			"MATTERCODEX_ARTIFACT_STORAGE_SECRET",
		} {
			if !strings.Contains(text, variable) {
				t.Errorf("%s не обрабатывает %s", relativePath, variable)
			}
		}
		if strings.Contains(text, "set -x") {
			t.Errorf("%s включает shell tracing", relativePath)
		}
		if strings.Contains(text, "05a-artifact-storage-secret.yaml") {
			t.Errorf("%s сохраняет отрендерованный artifact storage Secret на диск", relativePath)
		}
	}
}

package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
)

type deploymentSecuritySnapshot struct {
	RBAC            []rbacObjectSnapshot `json:"rbac"`
	Pod             podSpecSnapshot      `json:"pod"`
	NetworkPolicies []string             `json:"network_policies"`
}

type rbacObjectSnapshot struct {
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	Rules    []string `json:"rules,omitempty"`
	RoleRef  string   `json:"role_ref,omitempty"`
	Subjects []string `json:"subjects,omitempty"`
}

type podSpecSnapshot struct {
	ServiceAccount  string                      `json:"service_account"`
	SecurityContext map[string]any              `json:"security_context"`
	InitContainers  []containerSecuritySnapshot `json:"init_containers"`
	Containers      []containerSecuritySnapshot `json:"containers"`
	SecretVolumes   []string                    `json:"secret_volumes"`
}

type containerSecuritySnapshot struct {
	Name            string         `json:"name"`
	SecurityContext map[string]any `json:"security_context"`
	SecretRefs      []string       `json:"secret_refs"`
}

func TestDeploymentSecuritySnapshot(t *testing.T) {
	repoRoot := testRepositoryRoot(t)
	snapshot := deploymentSecuritySnapshot{
		RBAC:            parseRBACSnapshot(t, filepath.Join(repoRoot, "deploy/k8s/bot-service/rbac.yaml.tpl")),
		Pod:             parsePodSnapshot(t, filepath.Join(repoRoot, "deploy/k8s/bot-service/deployment.yaml.tpl")),
		NetworkPolicies: findManifestKinds(t, filepath.Join(repoRoot, "deploy/k8s"), "NetworkPolicy"),
	}
	actual, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v", err)
	}
	actual = append(actual, '\n')
	expected, err := os.ReadFile(filepath.Join("testdata", "deployment-security.snapshot.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(snapshot) error = %v", err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("снимок RBAC/PodSpec/Secret refs/NetworkPolicy изменился:\n--- expected\n%s\n--- actual\n%s", expected, actual)
	}
}

func TestBotServiceRuntimeSecretRBACMatchesCleanupProtocol(t *testing.T) {
	objects := decodeYAMLDocuments(t, filepath.Join(testRepositoryRoot(t), "deploy/k8s/bot-service/rbac.yaml.tpl"))
	wantVerbs := []string{"create", "get", "list", "update", "patch", "delete"}
	found := false
	for _, object := range objects {
		if stringValue(object["kind"]) != "Role" || nestedString(object, "metadata", "name") != "matter-codex-bot-service-runtime" {
			continue
		}
		for _, rawRule := range sliceValue(object["rules"]) {
			rule, _ := rawRule.(map[string]any)
			apiGroups := stringSlice(rule["apiGroups"])
			resources := stringSlice(rule["resources"])
			verbs := stringSlice(rule["verbs"])
			if slices.Contains(apiGroups, "*") || slices.Contains(resources, "*") || slices.Contains(verbs, "*") {
				t.Fatal("runtime Role must not contain wildcard permissions")
			}
			if slices.Equal(resources, []string{"secrets"}) {
				found = true
				if !slices.Equal(apiGroups, []string{""}) || !slices.Equal(verbs, wantVerbs) {
					t.Fatalf("Secret RBAC apiGroups=%v verbs=%v, want core/%v", apiGroups, verbs, wantVerbs)
				}
			}
		}
	}
	if !found {
		t.Fatal("runtime Role Secret rule was not found")
	}
}

func parseRBACSnapshot(t *testing.T, path string) []rbacObjectSnapshot {
	t.Helper()
	objects := decodeYAMLDocuments(t, path)
	result := make([]rbacObjectSnapshot, 0, len(objects))
	for _, object := range objects {
		kind := stringValue(object["kind"])
		if kind != "Role" && kind != "ClusterRole" && kind != "RoleBinding" && kind != "ClusterRoleBinding" {
			continue
		}
		item := rbacObjectSnapshot{Kind: kind, Name: nestedString(object, "metadata", "name")}
		for _, rawRule := range sliceValue(object["rules"]) {
			rule, _ := rawRule.(map[string]any)
			apiGroups := strings.Join(stringSlice(rule["apiGroups"]), ",")
			if apiGroups == "" {
				apiGroups = "core"
			}
			item.Rules = append(item.Rules,
				"apiGroups="+apiGroups+";resources="+strings.Join(stringSlice(rule["resources"]), ",")+";verbs="+strings.Join(stringSlice(rule["verbs"]), ","),
			)
		}
		if roleRef, ok := object["roleRef"].(map[string]any); ok {
			item.RoleRef = stringValue(roleRef["kind"]) + "/" + stringValue(roleRef["name"])
		}
		for _, rawSubject := range sliceValue(object["subjects"]) {
			subject, _ := rawSubject.(map[string]any)
			item.Subjects = append(item.Subjects, stringValue(subject["kind"])+"/"+stringValue(subject["name"]))
		}
		result = append(result, item)
	}
	return result
}

func parsePodSnapshot(t *testing.T, path string) podSpecSnapshot {
	t.Helper()
	objects := decodeYAMLDocuments(t, path)
	if len(objects) != 1 {
		t.Fatalf("deployment documents = %d, want 1", len(objects))
	}
	spec := nestedMap(objects[0], "spec", "template", "spec")
	result := podSpecSnapshot{
		ServiceAccount:  stringValue(spec["serviceAccountName"]),
		SecurityContext: mapValue(spec["securityContext"]),
	}
	result.InitContainers = parseContainerSnapshots(sliceValue(spec["initContainers"]))
	result.Containers = parseContainerSnapshots(sliceValue(spec["containers"]))
	for _, rawVolume := range sliceValue(spec["volumes"]) {
		volume, _ := rawVolume.(map[string]any)
		secret, _ := volume["secret"].(map[string]any)
		if name := stringValue(secret["secretName"]); name != "" {
			result.SecretVolumes = append(result.SecretVolumes, name)
		}
	}
	sort.Strings(result.SecretVolumes)
	return result
}

func parseContainerSnapshots(containers []any) []containerSecuritySnapshot {
	result := make([]containerSecuritySnapshot, 0, len(containers))
	for _, rawContainer := range containers {
		container, _ := rawContainer.(map[string]any)
		item := containerSecuritySnapshot{
			Name:            stringValue(container["name"]),
			SecurityContext: mapValue(container["securityContext"]),
		}
		for _, rawEnv := range sliceValue(container["env"]) {
			env, _ := rawEnv.(map[string]any)
			secretRef := nestedMap(env, "valueFrom", "secretKeyRef")
			if name := stringValue(secretRef["name"]); name != "" {
				item.SecretRefs = append(item.SecretRefs, name+"/"+stringValue(secretRef["key"]))
			}
		}
		sort.Strings(item.SecretRefs)
		result = append(result, item)
	}
	return result
}

func findManifestKinds(t *testing.T, root string, kind string) []string {
	t.Helper()
	result := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || (!strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yaml.tpl")) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), "kind: "+kind) {
			result = append(result, filepath.ToSlash(path[len(root)+1:]))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("filepath.WalkDir() error = %v", err)
	}
	sort.Strings(result)
	return result
}

func decodeYAMLDocuments(t *testing.T, path string) []map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v", path, err)
	}
	decoder := k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader(content), 4096)
	result := []map[string]any{}
	for {
		var object map[string]any
		if err := decoder.Decode(&object); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode YAML %s error = %v", path, err)
		}
		if len(object) > 0 {
			result = append(result, object)
		}
	}
	return result
}

func testRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() не вернул путь")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "../../../../.."))
}

func nestedMap(source map[string]any, path ...string) map[string]any {
	current := source
	for _, key := range path {
		next, _ := current[key].(map[string]any)
		current = next
	}
	return current
}

func nestedString(source map[string]any, path ...string) string {
	if len(path) == 0 {
		return ""
	}
	return stringValue(nestedMap(source, path[:len(path)-1]...)[path[len(path)-1]])
}

func mapValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return map[string]any{}
	}
	return result
}

func sliceValue(value any) []any {
	result, _ := value.([]any)
	return result
}

func stringSlice(value any) []string {
	items := sliceValue(value)
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, stringValue(item))
	}
	return result
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

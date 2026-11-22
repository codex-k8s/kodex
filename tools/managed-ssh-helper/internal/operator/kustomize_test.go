package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOperatorKustomizeConsumer exercises the real offline kubectl/yq consumer under an explicit opt-in, preserving the broker image and unrelated environment while mounting the exact immutable qualification and destination policy.
// TestOperatorKustomizeConsumer проверяет настоящий offline kubectl/yq consumer при явном opt-in, сохраняя broker image и unrelated environment с exact immutable qualification mount и destination policy.
func TestOperatorKustomizeConsumer(t *testing.T) {
	if os.Getenv("KODEX_SSH_OPERATOR_KUSTOMIZE") != "1" {
		t.Skip("offline Kubernetes render fixture is required")
	}
	document, now := operatorFixture(t)
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := Render(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	image := "synthetic.example.invalid/secret-broker@sha256:" + strings.Repeat("a", 64)
	base := map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": map[string]string{"name": "secret-broker", "namespace": "kodex-system"}, "spec": map[string]any{
		"selector": map[string]any{"matchLabels": map[string]string{"app.kubernetes.io/name": "secret-broker"}}, "template": map[string]any{"metadata": map[string]any{"labels": map[string]string{"app.kubernetes.io/name": "secret-broker", "app.kubernetes.io/component": "secret-broker"}}, "spec": map[string]any{"containers": []any{map[string]any{"name": "secret-broker", "image": image, "env": []map[string]string{{"name": "KEEP_EXISTING", "value": "preserved"}}}}}},
	}}
	encoded, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"broker.json": encoded, "qualification.json": projection.ConfigMap, "policy.json": projection.NetworkPolicy, "patch.json": projection.DeploymentPatch,
		"kustomization.yaml": []byte(`{"apiVersion":"kustomize.config.k8s.io/v1beta1","kind":"Kustomization","resources":["broker.json","qualification.json","policy.json"],"patches":[{"path":"patch.json"}]}`)}
	for name, raw := range files {
		if os.WriteFile(filepath.Join(directory, name), raw, 0600) != nil {
			t.Fatal("render fixture write failed")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "kubectl", "--kubeconfig", "/synthetic/unused-kubeconfig", "--context", "synthetic-offline", "kustomize", directory)
	rendered, err := command.Output()
	if err != nil {
		t.Fatal("offline Kubernetes render failed", err)
	}
	parser := exec.CommandContext(ctx, "yq", "-o=json", "-I=0", ".")
	parser.Stdin = bytes.NewReader(rendered)
	objects, err := parser.Output()
	if err != nil {
		t.Fatal("render JSON projection failed", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(objects))
	count := 0
	for {
		var object struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Data      map[string]string `json:"data"`
			Immutable bool              `json:"immutable"`
			Spec      struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Name  string `json:"name"`
							Image string `json:"image"`
							Env   []struct {
								Name  string `json:"name"`
								Value string `json:"value"`
							} `json:"env"`
							VolumeMounts []struct {
								MountPath string `json:"mountPath"`
								ReadOnly  bool   `json:"readOnly"`
							} `json:"volumeMounts"`
						} `json:"containers"`
						Volumes []struct {
							ConfigMap struct {
								Name        string `json:"name"`
								DefaultMode int    `json:"defaultMode"`
							} `json:"configMap"`
						} `json:"volumes"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		}
		err := decoder.Decode(&object)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal("invalid rendered object")
		}
		count++
		if object.Metadata.Namespace != "kodex-system" {
			t.Fatal("render crossed namespace")
		}
		switch object.Kind {
		case "ConfigMap":
			if object.Metadata.Name != projection.ConfigMapName || !object.Immutable || object.Data["profiles.json"] != string(raw) {
				t.Fatal("qualification ConfigMap changed")
			}
		case "Deployment":
			containers := object.Spec.Template.Spec.Containers
			if len(containers) != 1 || containers[0].Name != "secret-broker" || containers[0].Image != image {
				t.Fatal("broker identity or image changed")
			}
			env := map[string]string{}
			for _, item := range containers[0].Env {
				if _, ok := env[item.Name]; ok {
					t.Fatal("duplicate deployment setting")
				}
				env[item.Name] = item.Value
			}
			if env["KEEP_EXISTING"] != "preserved" || env["SECRET_BROKER_SSH_QUALIFICATION_SHA256"] != projection.ArtifactSHA256 {
				t.Fatal("broker environment binding changed")
			}
			if len(containers[0].VolumeMounts) != 1 || !containers[0].VolumeMounts[0].ReadOnly || len(object.Spec.Template.Spec.Volumes) != 1 || object.Spec.Template.Spec.Volumes[0].ConfigMap.Name != projection.ConfigMapName || object.Spec.Template.Spec.Volumes[0].ConfigMap.DefaultMode != 292 {
				t.Fatal("broker mount changed")
			}
		case "NetworkPolicy":
			if object.Metadata.Name != "secret-broker-ssh-qualified-targets" {
				t.Fatal("unexpected network policy")
			}
		default:
			t.Fatal("unexpected deployment object")
		}
	}
	if count != 3 {
		t.Fatal("incomplete offline render")
	}
}

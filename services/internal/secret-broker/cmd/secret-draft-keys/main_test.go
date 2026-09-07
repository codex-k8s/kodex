package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPrivateMaterialCommandsAndSafeCheck(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	var output bytes.Buffer
	if err := run([]string{"generate", "--output-file", first}, &output); err != nil || output.Len() != 0 {
		t.Fatal("generate failed or emitted material")
	}
	if err := run([]string{"rotate", "--input-file", first, "--output-file", second, "--expected-revision", "1"}, &output); err != nil || output.Len() != 0 {
		t.Fatal("rotate failed or emitted material")
	}
	if err := run([]string{"check", "--input-file", second}, &output); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if json.Unmarshal(output.Bytes(), &result) != nil || len(result) != 2 || result["revision"] != float64(2) {
		t.Fatal("check output is not safe summary")
	}
	digest, ok := result["digest"].(string)
	if !ok || len(digest) != 64 || strings.Contains(output.String(), "material") || strings.Contains(output.String(), "current") {
		t.Fatal("check exposed private fields")
	}
	raw, err := os.ReadFile(second)
	defer clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Keys []struct{ ID, Material string }
	}
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("invalid fixture")
	}
	for _, key := range doc.Keys {
		if strings.Contains(output.String(), key.ID) || strings.Contains(output.String(), key.Material) {
			t.Fatal("key identity or material exposed")
		}
	}
	copyPath := filepath.Join(dir, "copy.json")
	output.Reset()
	if err := run([]string{"copy", "--input-file", second, "--output-file", copyPath}, &output); err != nil || output.Len() != 0 {
		t.Fatal("private copy failed")
	}
	copied, err := os.ReadFile(copyPath)
	defer clear(copied)
	if err != nil || !bytes.Equal(copied, raw) {
		t.Fatal("private copy changed keyring")
	}
	if err := run([]string{"copy", "--input-file", first, "--output-file", copyPath}, &output); err == nil {
		t.Fatal("copy overwrote existing material")
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, raw, "", "  ") != nil {
		t.Fatal("fixture formatting failed")
	}
	prettyPath := filepath.Join(dir, "pretty.json")
	if os.WriteFile(prettyPath, pretty.Bytes(), 0o600) != nil {
		t.Fatal("fixture write failed")
	}
	prettyCopy := filepath.Join(dir, "pretty-copy.json")
	if err := run([]string{"copy", "--input-file", prettyPath, "--output-file", prettyCopy}, &output); err != nil {
		t.Fatal("formatted private copy failed")
	}
	canonical, err := os.ReadFile(prettyCopy)
	defer clear(canonical)
	if err != nil || !bytes.Equal(canonical, raw) {
		t.Fatal("formatted copy changed key identities")
	}
}

func TestGuardCheckUsesRuntimeParserAndSafeSummary(t *testing.T) {
	object := corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "secret-broker-draft-key-guard", Namespace: "kodex-secret-drafts", UID: "fixture", ResourceVersion: "1", Labels: map[string]string{"app.kubernetes.io/managed-by": "kodex-secret-broker-bootstrap", "kodex.dev/purpose": "secret-draft-key-guard"}}, Data: map[string]string{"state.json": `{"v":1,"manifest":null,"uses":[]}`}}
	for _, state := range []string{`{"v":1,"manifest":null,"uses":[]}`, `{"v":1,"manifest":null,"uses":[{"id":"private-marker","generation":1,"encryptions":0}]}`, `{"v":1,"manifest":null,"uses":[],"unknown":"private-marker"}`} {
		object.Data["state.json"] = state
		raw, _ := json.Marshal(object)
		var output bytes.Buffer
		err := checkGuard(bytes.NewReader(raw), &output)
		if state == `{"v":1,"manifest":null,"uses":[]}` {
			if err != nil || output.String() != "null\n" {
				t.Fatal("genesis guard check failed")
			}
		} else if err == nil || output.Len() != 0 {
			t.Fatal("invalid retained guard accepted or exposed")
		}
	}
}

func TestCommandsRejectInvalidArgumentsWithoutEcho(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown-private-marker"}, {"generate", "--value=private-marker"}, {"generate"}, {"rotate", "--input-file=private-marker", "--output-file=/tmp/no-output"}, {"check", "--input-file=private-marker"}, {"check", "--input-file=private-marker", "extra-private-marker"}} {
		var output bytes.Buffer
		err := run(args, &output)
		if err == nil || output.Len() != 0 || strings.Contains(err.Error(), "private-marker") {
			t.Fatal("invalid command exposed its arguments")
		}
	}
}

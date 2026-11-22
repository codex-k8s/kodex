package managedkubernetes

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestCopyProfileDirectionAndNativeRecipe proves source-only artifact coordinates and literal native argv.
// TestCopyProfileDirectionAndNativeRecipe доказывает source-only artifact coordinates и literal native argv.
func TestCopyProfileDirectionAndNativeRecipe(t *testing.T) {
	for _, test := range []struct {
		operation, format, remote string
		command                   []string
	}{
		{"kubernetes.cp.upload", "FILE", "/tmp/file name", []string{"tar", "-xmf", "-", "-C", "/tmp"}},
		{"kubernetes.cp.upload", "TAR", "/tmp/directory", []string{"tar", "-xmf", "-", "-C", "/tmp/directory"}},
		{"kubernetes.cp.download", "FILE", "--checkpoint=1", []string{"tar", "-cf", "-", "-C", ".", "./--checkpoint=1"}},
		{"kubernetes.cp.download", "TAR", "/tmp/$(command)", []string{"tar", "-cf", "-", "-C", "/tmp", "./$(command)"}},
		{"kubernetes.cp.download", "TAR", "/", []string{"tar", "-cf", "-", "-C", "/", "."}},
	} {
		profile := CopyProfile{TransferFormat: test.format, RemotePath: test.remote, MaximumBytes: 65536, TimeoutSeconds: 10}
		if test.operation == "kubernetes.cp.upload" {
			profile.ArtifactRef, profile.ArtifactSHA256 = "artifact_synthetic", strings.Repeat("a", 64)
		}
		input := copyProfileInput(t, profile)
		raw, err := EncodeOperationInput(test.operation, input)
		if err != nil {
			t.Fatal(err)
		}
		decoded, _, err := DecodeOperationInput(test.operation, raw)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := DecodeCopyProfile(test.operation, decoded)
		if err != nil || actual != profile {
			t.Fatal("copy profile changed across canonical input")
		}
		command, err := actual.NativeCommand(test.operation)
		if err != nil || !reflect.DeepEqual(command, test.command) {
			t.Fatal("native copy argv changed")
		}
	}
}

// copyProfileInput builds an exact synthetic Pod witness without assigning authority.
// copyProfileInput строит exact synthetic Pod witness без назначения authority.
func copyProfileInput(t *testing.T, profile CopyProfile) OperationInput {
	t.Helper()
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	return OperationInput{Version: "v1", Resource: "pods", Namespace: "example", Name: "web", UID: "dc7b5a7f-2421-41bf-bf3b-71f7b362fda8", ResourceVersion: "17", Options: raw}
}

// TestCopyProfileRejectsAmbiguousOrCallerOwnedDownload closes paths and upload/download ownership before command creation.
// TestCopyProfileRejectsAmbiguousOrCallerOwnedDownload закрывает пути и upload/download ownership до создания command.
func TestCopyProfileRejectsAmbiguousOrCallerOwnedDownload(t *testing.T) {
	profile := CopyProfile{TransferFormat: "FILE", RemotePath: "/tmp/file", MaximumBytes: 65536, TimeoutSeconds: 10}
	for _, remote := range []string{"", "../file", "a/../file", "/tmp//file", "/tmp/./file", "/tmp/file/", "/tmp/\nfile", "/", "."} {
		invalid := profile
		invalid.RemotePath = remote
		if _, err := invalid.NativeCommand("kubernetes.cp.download"); err == nil {
			t.Fatal("unsafe path acquired a native recipe")
		}
	}
	if _, err := profile.NativeCommand("kubernetes.cp.upload"); err == nil {
		t.Fatal("upload without source pins accepted")
	}
	for _, change := range []func(*CopyProfile){
		func(p *CopyProfile) { p.ArtifactRef = "artifact_synthetic" },
		func(p *CopyProfile) { p.ArtifactSHA256 = strings.Repeat("a", 64) },
		func(p *CopyProfile) { p.TransferFormat = "RAW_COMMAND" },
		func(p *CopyProfile) { p.MaximumBytes = 17 << 20 },
		func(p *CopyProfile) { p.TimeoutSeconds = 301 },
	} {
		invalid := profile
		change(&invalid)
		input := copyProfileInput(t, invalid)
		raw, _ := EncodeOperationInput("kubernetes.cp.download", input)
		if _, _, err := DecodeOperationInput("kubernetes.cp.download", raw); err == nil {
			t.Fatal("invalid copy options escaped public input boundary")
		}
	}
	input := copyProfileInput(t, profile)
	input.Resource = "secrets"
	if _, err := DecodeCopyProfile("kubernetes.cp.download", input); err == nil {
		t.Fatal("non-Pod target accepted")
	}
	input = copyProfileInput(t, profile)
	input.Options = append(input.Options[:len(input.Options)-1], []byte(`,"command":["sh"]}`)...)
	if _, err := DecodeCopyProfile("kubernetes.cp.download", input); err == nil {
		t.Fatal("caller command accepted")
	}
}

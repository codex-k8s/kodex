package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

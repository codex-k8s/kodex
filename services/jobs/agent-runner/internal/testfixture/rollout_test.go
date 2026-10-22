package testfixture

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteRolloutSourcePreservesOriginalBytesAndFlags independently checks the two JSONL records and restricted new-file modes, then verifies an existing file keeps its mode and trailing bytes because the original writer did not truncate.
// TestWriteRolloutSourcePreservesOriginalBytesAndFlags независимо проверяет две JSONL-записи и ограниченные режимы нового файла, затем сохранение режима и хвостовых байтов существующего файла, поскольку прежний writer не выполнял truncation.
func TestWriteRolloutSourcePreservesOriginalBytesAndFlags(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions", "day", "rollout.jsonl")
			want := []byte("{\"type\":\"session_meta\"}\n{\"type\":\"event_msg\"}\n")
			if existing {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				want = append(want, bytes.Repeat([]byte("x"), 32)...)
				if err := os.WriteFile(path, want, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			WriteRolloutSource(t, path)
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatal("synthetic rollout bytes changed")
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&^0o640 != 0 {
				t.Fatal("synthetic rollout mode widened")
			}
			if existing && info.Mode().Perm() != 0o600 {
				t.Fatal("existing synthetic rollout mode changed")
			}
			parent, err := os.Stat(filepath.Dir(path))
			if err != nil || !parent.IsDir() || parent.Mode().Perm()&^0o700 != 0 {
				t.Fatal("synthetic rollout directory mode widened")
			}
		})
	}
}

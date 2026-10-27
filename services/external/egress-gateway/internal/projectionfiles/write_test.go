package projectionfiles

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestWritePrivateArtifacts checks exact producer bytes, private modes and empty initial output
// using ordinary local files; no producer, DNS request or deployment is started.
// TestWritePrivateArtifacts проверяет точные bytes producer, закрытые права и пустой начальный
// output на обычных локальных файлах; producer, DNS request и deploy не запускаются.
func TestWritePrivateArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string][]byte
	}{
		{name: "files", files: map[string][]byte{"projection.json": []byte("{}\n"), "network-policy.yaml": []byte("kind: NetworkPolicy\n")}},
		{name: "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "output")
			if err := Write(directory, tc.files); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(directory)
			if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
				t.Fatal("output directory mode changed")
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != len(tc.files) {
				t.Fatal("output artifact count changed")
			}
			for name, want := range tc.files {
				file := filepath.Join(directory, name)
				got, err := os.ReadFile(file)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatal("artifact bytes changed")
				}
				info, err := os.Stat(file)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("artifact mode changed")
				}
			}
		})
	}
}

// TestWriteRefusesExistingOutput requires the original closed directory failure and unchanged
// existing bytes for both a directory and a regular file occupying the requested output path.
// TestWriteRefusesExistingOutput требует прежний закрытый отказ создания directory и сохранение
// существующих bytes для directory и regular file по запрошенному output path.
func TestWriteRefusesExistingOutput(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directory], func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "output")
			file := output
			if directory {
				if err := os.Mkdir(output, 0700); err != nil {
					t.Fatal(err)
				}
				file = filepath.Join(output, "sentinel")
			}
			if err := os.WriteFile(file, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			err := Write(output, map[string][]byte{"sentinel": []byte("replacement")})
			if err == nil || err.Error() != "create projection output directory" {
				t.Fatal("existing output failure changed")
			}
			value, err := os.ReadFile(file)
			if err != nil || string(value) != "original" {
				t.Fatal("existing output was overwritten")
			}
		})
	}
}

// TestWritePreservesFailedOutput checks the native non-atomic behavior: an artifact error leaves
// the newly created directory for the caller to inspect or clean, with the original safe error.
// TestWritePreservesFailedOutput проверяет прежнее неатомарное поведение: ошибка артефакта оставляет
// новый directory для проверки либо очистки caller и возвращает прежнюю безопасную error.
func TestWritePreservesFailedOutput(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "output")
	err := Write(directory, map[string][]byte{"": []byte("fixture")})
	if err == nil || err.Error() != "write projection artifact" {
		t.Fatal("artifact failure changed")
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		t.Fatal("failed output directory was removed")
	}
}

// TestWriteDoesNotCreateParents preserves Mkdir rather than MkdirAll semantics and prevents a
// failed command from silently creating a wider output tree.
// TestWriteDoesNotCreateParents сохраняет семантику Mkdir вместо MkdirAll и не позволяет
// неуспешной команде незаметно создать более широкое дерево output.
func TestWriteDoesNotCreateParents(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "missing")
	err := Write(filepath.Join(parent, "output"), nil)
	if err == nil || err.Error() != "create projection output directory" {
		t.Fatal("missing parent failure changed")
	}
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatal("missing parent was created")
	}
}

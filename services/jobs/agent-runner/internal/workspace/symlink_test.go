package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestWorkLinkConfinement exercises npm-style links and hostile link targets
// against the real kernel resolver without reading any target file contents.
// TestWorkLinkConfinement проверяет npm-ссылки и опасные цели настоящим
// механизмом ядра, не читая содержимое файлов целей.
func TestWorkLinkConfinement(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules/pkg/bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "node_modules/.bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules/pkg/bin/cli"), []byte("synthetic"), 0o000); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	for _, tc := range []struct {
		name, target string
		allowed      bool
	}{
		{"npm", "../pkg/bin/cli", true}, {"directory", "../pkg", true},
		{"absolute", filepath.Join(root, "node_modules/pkg/bin/cli"), false},
		{"escape", "../../../outside", false}, {"missing", "../missing", false},
		{"loop", "loop", false}, {"magic", "/proc/self/fd/0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			relative := "node_modules/.bin/" + tc.name
			if err := os.Symlink(tc.target, filepath.Join(root, relative)); err != nil {
				t.Fatal(err)
			}
			if err := CheckWorkLink(int(dir.Fd()), relative); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v: %v", tc.allowed, err)
			}
		})
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../fifo", filepath.Join(root, "node_modules/.bin/special")); err != nil {
		t.Fatal(err)
	}
	if CheckWorkLink(int(dir.Fd()), "node_modules/.bin/special") == nil {
		t.Fatal("special target accepted")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "foreign")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../foreign", filepath.Join(root, "node_modules/.bin/chained")); err != nil {
		t.Fatal(err)
	}
	if CheckWorkLink(int(dir.Fd()), "node_modules/.bin/chained") == nil {
		t.Fatal("escaping chain accepted")
	}
}

// TestCanaryDependencyLinks permits confined work links but counts them against
// quota and never permits links in the publication outbox.
// TestCanaryDependencyLinks допускает внутренние ссылки work, учитывает их в
// квоте и сохраняет запрет ссылок в каталоге публикации outbox.
func TestCanaryDependencyLinks(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"work/node_modules/.bin", "work/node_modules/pkg", ".kodex/outbox"} {
		if err := os.MkdirAll(filepath.Join(root, relative), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "work/node_modules/pkg/cli"), []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../pkg/cli", filepath.Join(root, "work/node_modules/.bin/cli")); err != nil {
		t.Fatal(err)
	}
	if err := RunCanary(t.Context(), root, testPolicy()); err != nil {
		t.Fatal(err)
	}
	size, files, err := writableUsage(root, testPolicy())
	if err != nil || size != int64(len("synthetic")+len("../pkg/cli")) || files != 2 {
		t.Fatalf("link quota omitted: %d %d %v", size, files, err)
	}
	if err := os.WriteFile(filepath.Join(root, ".kodex/outbox/report"), []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("report", filepath.Join(root, ".kodex/outbox/link")); err != nil {
		t.Fatal(err)
	}
	if RunCanary(t.Context(), root, testPolicy()) == nil {
		t.Fatal("outbox link accepted")
	}
}

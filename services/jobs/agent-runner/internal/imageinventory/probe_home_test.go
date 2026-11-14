package imageinventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestProbeHomeSealedAndRemoved verifies empty startup directories are read-only and parent cleanup removes only its disposable home.
// TestProbeHomeSealedAndRemoved проверяет отсутствие записи в пустые каталоги запуска и очистку только созданного временного HOME.
func TestProbeHomeSealedAndRemoved(t *testing.T) {
	home, cleanup, err := prepareProbeHome()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if !probeHomeValid(home, uint32(os.Geteuid())) {
		t.Fatal("sealed home rejected")
	}
	for _, directory := range probeHomeDirectories {
		if info, err := os.Stat(filepath.Join(home, directory)); err != nil || !info.IsDir() || info.Mode().Perm() != 0555 {
			t.Fatal("startup directory is not sealed")
		}
	}
	cleanup()
	if _, err := os.Lstat(home); !os.IsNotExist(err) {
		t.Fatal("disposable home retained")
	}
}

// TestProbeHomeRejectsUnsafeTrees tests ownership, writes, files, symlinks and excessive traversal against an otherwise valid tree.
// TestProbeHomeRejectsUnsafeTrees проверяет владельца, запись, файлы, ссылки и чрезмерный обход на исходно допустимом дереве.
func TestProbeHomeRejectsUnsafeTrees(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"writable": func(t *testing.T, home string) { _ = os.Chmod(home, 0755) },
		"file": func(t *testing.T, home string) {
			_ = os.Chmod(home, 0700)
			if err := os.WriteFile(filepath.Join(home, "configuration"), []byte("synthetic"), 0400); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(home, 0555)
		},
		"symlink": func(t *testing.T, home string) {
			_ = os.Chmod(home, 0700)
			if err := os.Symlink("/nonexistent", filepath.Join(home, "configuration")); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(home, 0555)
		},
		"oversized": func(t *testing.T, home string) {
			_ = os.Chmod(home, 0700)
			for index := range 33 {
				if err := os.Mkdir(filepath.Join(home, string(rune('A'+index))), 0555); err != nil {
					t.Fatal(err)
				}
			}
			_ = os.Chmod(home, 0555)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			home, cleanup, err := prepareProbeHome()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			if !probeHomeValid(home, uint32(os.Geteuid())) || probeHomeValid(home, uint32(os.Geteuid()+1)) {
				t.Fatal("ownership precondition failed")
			}
			mutate(t, home)
			if probeHomeValid(home, uint32(os.Geteuid())) {
				t.Fatal("unsafe probe home accepted")
			}
		})
	}
	for _, home := range []string{"", "/tmp", "/nonexistent", "/tmp/kodex-image-probe-", "/tmp/kodex-image-probe-name", "/tmp/kodex-image-probe-1/../2"} {
		if probeHomeValid(home, uint32(os.Geteuid())) {
			t.Fatal("caller path accepted")
		}
	}
}

// TestProbeHomeOnlyForOpenCode preserves the fixed nonexistent HOME of every other registered version command.
// TestProbeHomeOnlyForOpenCode сохраняет фиксированный несуществующий HOME для всех остальных зарегистрированных команд версии.
func TestProbeHomeOnlyForOpenCode(t *testing.T) {
	for _, probe := range runtimecontract.ImageToolProbes() {
		for _, path := range probe.Paths {
			if probeNeedsHome(path) != (probe.Name == "opencode") {
				t.Fatal("unexpected probe home selection")
			}
		}
	}
	if probeNeedsHome("/tmp/opencode") {
		t.Fatal("unregistered executable selected")
	}
}

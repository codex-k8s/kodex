package imageinventory

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// probeHomePrefix names disposable, credential-free inventory homes outside the observed image tree.
// probeHomePrefix задаёт имена временных каталогов инвентаризации без credentials вне наблюдаемого дерева образа.
const probeHomePrefix = "kodex-image-probe-"

// probeHomeDirectories covers the startup mkdir calls made before OpenCode 1.19.5 handles --version.
// probeHomeDirectories покрывает создание каталогов при запуске OpenCode 1.19.5 до обработки --version.
var probeHomeDirectories = []string{
	".cache/opencode/bin", ".local/share/opencode/log", ".local/share/opencode/repos",
	".config/opencode", ".local/state/opencode", "tmp/opencode",
}

// probeNeedsHome selects only registered OpenCode executable paths; closedProbeArguments still checks the complete command.
// probeNeedsHome выбирает только зарегистрированные пути OpenCode; closedProbeArguments отдельно проверяет полную команду.
func probeNeedsHome(path string) bool {
	for _, probe := range runtimecontract.ImageToolProbes() {
		if probe.Name == "opencode" && slices.Contains(probe.Paths, path) {
			return true
		}
	}
	return false
}

// prepareProbeHome creates empty startup directories and seals them before the unprivileged probe starts; cleanup belongs to its waiting parent.
// prepareProbeHome создаёт пустые каталоги запуска и запрещает запись до старта непривилегированной пробы; очистку выполняет ожидающий родитель.
func prepareProbeHome() (string, func(), error) {
	home, err := os.MkdirTemp("/tmp", probeHomePrefix)
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				return os.Chmod(path, 0700)
			}
			return err
		})
		_ = os.RemoveAll(home)
	}
	for _, directory := range probeHomeDirectories {
		if err = os.MkdirAll(filepath.Join(home, directory), 0700); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	err = filepath.WalkDir(home, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chmod(path, 0555)
	})
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return home, cleanup, nil
}

// probeHomeValid accepts a bounded tree of owner-owned read-only directories without files or symlinks, never an arbitrary caller HOME.
// probeHomeValid принимает ограниченное дерево каталогов указанного владельца без записи, файлов и ссылок, а не произвольный HOME вызывающего.
func probeHomeValid(home string, owner uint32) bool {
	if filepath.Dir(home) != "/tmp" || !strings.HasPrefix(filepath.Base(home), probeHomePrefix) || home != filepath.Clean(home) {
		return false
	}
	suffix := strings.TrimPrefix(filepath.Base(home), probeHomePrefix)
	if suffix == "" || strings.IndexFunc(suffix, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return false
	}
	count := 0
	return filepath.WalkDir(home, func(_ string, entry fs.DirEntry, err error) error {
		count++
		if err != nil || count > 32 || !entry.IsDir() {
			return errProbeSandboxFilesystem
		}
		info, err := entry.Info()
		if err != nil {
			return errProbeSandboxFilesystem
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != owner || info.Mode() != fs.ModeDir|0555 {
			return errProbeSandboxFilesystem
		}
		return nil
	}) == nil
}

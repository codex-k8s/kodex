package imageinventory

import (
	"context"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestProbeDoesNotInventMissingAndIgnoresOutsideSymlinks checks a disposable root with an outside symlink remains MISSING with no fabricated version/hash/path and no probe execution.
// TestProbeDoesNotInventMissingAndIgnoresOutsideSymlinks проверяет сохранение MISSING без выдуманной версии, хеша и пути, без выполнения пробы для одноразового корня со ссылкой наружу.
func TestProbeDoesNotInventMissingAndIgnoresOutsideSymlinks(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "usr/bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/PRIVATE_SENTINEL", filepath.Join(directory, "usr/bin/git")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	probe := runtimecontract.ImageToolProbe{Name: "git", Paths: []string{"/usr/bin/git"}, Args: []string{"--version"}, Required: true}
	item := observe(context.Background(), root, probe)
	if item.Status != "MISSING" || item.Path != "" || item.Version != "" || item.SHA256 != "" || !item.Required {
		t.Fatal("missing tool was fabricated")
	}
}

// TestVersionProjectionDropsRawOutputAndBounds checks sample tool banners expose only numeric version tokens and an oversized capture keeps exactly 4096 bytes with sticky overflow.
// TestVersionProjectionDropsRawOutputAndBounds проверяет извлечение из примерных баннеров только числовых токенов версии и сохранение ровно 4096 байт слишком большого вывода с признаком переполнения.
func TestVersionProjectionDropsRawOutputAndBounds(t *testing.T) {
	for _, raw := range []string{"go version go1.26.6 linux/amd64 PRIVATE_SENTINEL", "git version 2.53.0 PRIVATE_SENTINEL", "v1.2.3\nPRIVATE_SENTINEL"} {
		match := versionPattern.FindStringSubmatch(raw)
		if len(match) != 2 || strings.Contains(match[1], "PRIVATE") || strings.Contains(match[1], " ") {
			t.Fatal("raw probe output escaped version projection")
		}
	}
	var output boundedOutput
	raw := make([]byte, 8192)
	n, err := output.Write(raw)
	if n != len(raw) || err != nil || output.Len() != 4096 || !output.overflow {
		t.Fatal("output budget is not bounded")
	}
}

// TestProbeModeRejectsCallerArgumentsBeforeReadingRuntime checks malformed inventory arguments fail before root/input/file observation; no actual inventory or tool command is run.
// TestProbeModeRejectsCallerArgumentsBeforeReadingRuntime проверяет отказ для неверных аргументов инвентаризации до наблюдения корня, входа или файлов; настоящая инвентаризация и команда инструмента не запускаются.
func TestProbeModeRejectsCallerArgumentsBeforeReadingRuntime(t *testing.T) {
	if Run(context.Background(), []string{"runner", Mode, "arbitrary"}) == nil {
		t.Fatal("unknown probe arguments accepted")
	}
}

// TestBuildInfoFallbackRequiresExactExecutableAndSuccessfulReadiness checks both permitted build-info fallbacks require successful readiness, exact unreplaced module/command and canonical version text.
// Missing, development, foreign and malformed metadata never become an observed tool version.
// TestBuildInfoFallbackRequiresExactExecutableAndSuccessfulReadiness проверяет для обоих разрешённых резервных путей build-info успешную готовность, точный неподменённый модуль и команду, канонический текст версии.
// Отсутствующие, разработческие, чужие и некорректные метаданные не становятся наблюдённой версией инструмента.
func TestBuildInfoFallbackRequiresExactExecutableAndSuccessfulReadiness(t *testing.T) {
	for _, name := range []string{"goimports", "grpcurl"} {
		module := "golang.org/x/tools"
		version := "v0.46.0"
		if name == "grpcurl" {
			module, version = "github.com/fullstorydev/grpcurl", "v1.9.3"
		}
		fixture := func() *debug.BuildInfo {
			return &debug.BuildInfo{Path: module + "/cmd/" + name,
				Main: debug.Module{Path: module, Version: version}}
		}
		if got, ok := buildInfoProbeVersion(name, fixture(), true); !ok || got != strings.TrimPrefix(version, "v") {
			t.Fatalf("successful exact executable %s lost its observed version", name)
		}
		for _, tc := range []struct {
			label string
			ready bool
			info  *debug.BuildInfo
		}{
			{"failed readiness", false, fixture()},
			{"missing metadata", true, nil},
		} {
			if got, ok := buildInfoProbeVersion(name, tc.info, tc.ready); ok || got != "" {
				t.Fatalf("%s invented a version for %s", tc.label, name)
			}
		}
		for _, bad := range []string{"", "(devel)", "dev build <no version set>", "1.9.3", "v1.9.3 PRIVATE_SENTINEL", "PRIVATE_SENTINEL v1.9.3"} {
			info := fixture()
			info.Main.Version = bad
			if got, ok := buildInfoProbeVersion(name, info, true); ok || got != "" {
				t.Fatalf("unobserved or malformed version accepted for %s", name)
			}
		}
		for _, mutate := range []func(*debug.BuildInfo){
			func(info *debug.BuildInfo) { info.Main.Path = "foreign/module" },
			func(info *debug.BuildInfo) { info.Path = module + "/cmd/other" },
			func(info *debug.BuildInfo) { info.Main.Replace = &debug.Module{Path: "foreign/module"} },
		} {
			info := fixture()
			mutate(info)
			if got, ok := buildInfoProbeVersion(name, info, true); ok || got != "" {
				t.Fatalf("foreign executable provenance accepted for %s", name)
			}
		}
	}
	if got, ok := buildInfoProbeVersion("foreign-tool", &debug.BuildInfo{}, true); ok || got != "" {
		t.Fatal("unknown fallback tool accepted")
	}
}

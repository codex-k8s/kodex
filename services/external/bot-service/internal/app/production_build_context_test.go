package app

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductionBuildContextsContainArtifactTypeAndBuild(t *testing.T) {
	repositoryRoot := productionContextRepositoryRoot(t)
	installScript := readProductionContextFile(t, filepath.Join(repositoryRoot, "scripts/remote/install-bot-service.sh"))
	tests := []struct {
		name         string
		dockerfile   string
		dockerCopy   string
		archiveStart string
		archiveEnd   string
		contextPaths []string
		buildPackage string
	}{
		{
			name: "bot-service", dockerfile: "services/external/bot-service/Dockerfile",
			dockerCopy: "COPY libs/go/artifacttype/ libs/go/artifacttype/", archiveStart: "BOT_SERVICE_ARCHIVE=", archiveEnd: "if [ \"$MATTERCODEX_IMAGE_BUILD_STRATEGY\"",
			contextPaths: []string{"go.mod", "go.sum", "libs/go/artifacttype", "libs/go/i18n", "services/external/bot-service"},
			buildPackage: "./services/external/bot-service/cmd/bot-service",
		},
		{
			name: "agent-runner", dockerfile: "services/jobs/agent-runner/Dockerfile",
			dockerCopy: "COPY libs/go/artifacttype ./libs/go/artifacttype", archiveStart: "AGENT_RUNNER_ARCHIVE=", archiveEnd: "if [ \"$MATTERCODEX_IMAGE_BUILD_STRATEGY\"",
			contextPaths: []string{"go.mod", "go.sum", "libs/go/artifacttype", "services/jobs/agent-runner"},
			buildPackage: "./services/jobs/agent-runner/cmd/agent-runner",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dockerfile := readProductionContextFile(t, filepath.Join(repositoryRoot, test.dockerfile))
			if !strings.Contains(dockerfile, test.dockerCopy) {
				t.Fatalf("production Dockerfile не копирует libs/go/artifacttype: %s", test.dockerfile)
			}
			archiveBlock := boundedProductionContextBlock(t, installScript, test.archiveStart, test.archiveEnd)
			if !strings.Contains(archiveBlock, "libs/go/artifacttype") {
				t.Fatal("remote production archive не содержит libs/go/artifacttype")
			}
			contextRoot := t.TempDir()
			for _, relative := range test.contextPaths {
				copyProductionContextPath(t, repositoryRoot, contextRoot, relative)
			}
			if _, err := os.Stat(filepath.Join(contextRoot, "libs/go/artifacttype/formats.go")); err != nil {
				t.Fatalf("сформированный production context не содержит artifacttype: %v", err)
			}
			command := exec.Command("go", "build", "-mod=readonly", "-trimpath", "-o", filepath.Join(contextRoot, "out"), test.buildPackage)
			command.Dir = contextRoot
			command.Env = append(filteredProductionBuildEnvironment(os.Environ()), "GOENV=off", "GOWORK=off", "GOFLAGS=")
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("герметичная сборка exact production context завершилась ошибкой: %v\n%s", err, output)
			}
		})
	}
}

func productionContextRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь теста production context")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(currentFile), "../../../../.."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func readProductionContextFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func boundedProductionContextBlock(t *testing.T, body string, start string, end string) string {
	t.Helper()
	startIndex := strings.Index(body, start)
	if startIndex < 0 {
		t.Fatalf("не найдено начало production archive: %s", start)
	}
	endIndex := strings.Index(body[startIndex:], end)
	if endIndex < 0 {
		t.Fatalf("не найден конец production archive: %s", end)
	}
	return body[startIndex : startIndex+endIndex]
}

func copyProductionContextPath(t *testing.T, sourceRoot string, targetRoot string, relative string) {
	t.Helper()
	source := filepath.Join(sourceRoot, relative)
	target := filepath.Join(targetRoot, relative)
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		copyProductionContextFile(t, source, target, info.Mode())
		return
	}
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relativePath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(targetRoot, relativePath)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		copyProductionContextFile(t, path, destination, entryInfo.Mode())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func copyProductionContextFile(t *testing.T, source string, target string, mode os.FileMode) {
	t.Helper()
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, body, mode.Perm()); err != nil {
		t.Fatal(err)
	}
}

func filteredProductionBuildEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, item := range environment {
		if strings.HasPrefix(item, "GOENV=") || strings.HasPrefix(item, "GOWORK=") || strings.HasPrefix(item, "GOFLAGS=") || strings.HasPrefix(item, "GOPROXY=") {
			continue
		}
		result = append(result, item)
	}
	return result
}

package internalrpcauth_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectProseAvoidsKnownMixedLanguagePhrases(t *testing.T) {
	root := repositoryRoot(t)
	files := []string{
		"contracts/README.md",
		"contracts/authorization/README.md",
		"docs/guides/distributed-security.md",
		"docs/runbooks/internal-rpc-authority.md",
		"libs/go/grpcserver/README.md",
		"libs/go/internalrpcauth/README.md",
		"libs/go/observability/README.md",
		"libs/go/serviceruntime/README.md",
		"services/internal/README.md",
		"services/internal/internal-rpc-authority/README.md",
	}
	forbidden := []string{
		"contract milestone",
		"full security path",
		"runtime binaries",
		"source of truth",
		"error/recovery boundary",
		"process lifecycle",
		"# observability runtime",
		"# internal rpc authentication",
		"security unit",
		"runtime-сценарий",
	}
	for _, relative := range files {
		prose := markdownProse(t, filepath.Join(root, relative))
		for _, phrase := range forbidden {
			if strings.Contains(strings.ToLower(prose), phrase) {
				t.Errorf("%s contains forbidden mixed-language phrase %q", relative, phrase)
			}
		}
	}
}

func markdownProse(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open Markdown document %s: %v", path, err)
	}
	defer file.Close()

	var result strings.Builder
	scanner := bufio.NewScanner(file)
	inFence := false
	inFrontmatter := false
	frontmatterDone := false
	for scanner.Scan() {
		line := scanner.Text()
		if !frontmatterDone && line == "---" {
			inFrontmatter = !inFrontmatter
			if !inFrontmatter {
				frontmatterDone = true
			}
			continue
		}
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFrontmatter || inFence {
			continue
		}
		for {
			start := strings.IndexByte(line, '`')
			if start < 0 {
				result.WriteString(line)
				result.WriteByte('\n')
				break
			}
			result.WriteString(line[:start])
			line = line[start+1:]
			end := strings.IndexByte(line, '`')
			if end < 0 {
				break
			}
			line = line[end+1:]
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan Markdown document %s: %v", path, err)
	}
	return result.String()
}

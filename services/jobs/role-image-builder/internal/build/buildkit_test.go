package build

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildKitReadinessUsesSupportedTinyLocalExport(t *testing.T) {
	t.Parallel()
	dockerfile := string(buildKitReadinessDockerfile(
		"registry.example/kodex/dockerfile",
		strings.Repeat("a", 64),
		"registry.example/kodex/agent-runner",
		"sha256:"+strings.Repeat("b", 64),
	))
	for _, required := range []string{
		"FROM registry.example/kodex/agent-runner@sha256:" + strings.Repeat("b", 64) + " AS verify",
		"test -x /usr/local/bin/kodex-init",
		"test -x /usr/local/bin/kodex-agent-runner",
		"printf ready > /tmp/kodex-readiness",
		"FROM scratch",
		"COPY --from=verify /tmp/kodex-readiness /kodex-readiness",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Fatalf("readiness Dockerfile does not contain %q", required)
		}
	}
	if strings.Contains(dockerfile, "cacheonly") {
		t.Fatal("readiness Dockerfile references unsupported cacheonly exporter")
	}

	outputDirectory := filepath.Join(t.TempDir(), "output")
	if got, want := buildKitReadinessOutput(outputDirectory), "type=local,dest="+outputDirectory; got != want {
		t.Fatalf("readiness output = %q, want %q", got, want)
	}
}

package build

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestNativeInventoryProbePreservesIndependentImmutablePins(t *testing.T) {
	input := &controlplanev1.RoleImageBuildInput{
		Dockerfile:                "FROM registry.invalid/owner@sha256:" + strings.Repeat("a", 64),
		SpecSha256:                strings.Repeat("b", 64),
		ImmutableBuildSha256:      strings.Repeat("c", 64),
		RoleRuntimeContractSha256: strings.Repeat("d", 64),
		FrontendSha256:            strings.Repeat("e", 64),
	}
	text := string(dockerfile(input, "registry.invalid/frontend", "registry.invalid/trusted", "sha256:"+strings.Repeat("f", 64)))
	stage := strings.Split(text, "FROM kodex-final-rootfs AS kodex-tool-probe\n")
	if len(stage) != 2 {
		t.Fatal("native inventory stage must be unique")
	}
	lines := strings.Split(stage[1], "\n")
	if len(lines) < 5 || lines[0] != "USER root" {
		t.Fatal("native inventory parent contract is missing")
	}
	prefix := "RUN --network=none --mount=type=bind,from=kodex-final-rootfs,source=/,target=/image,readonly "
	if !strings.HasPrefix(lines[1], prefix) {
		t.Fatal("native inventory probe must use the read-only finalized rootfs")
	}
	var command []string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], prefix)), &command); err != nil {
		t.Fatal("inventory probe must execute a closed JSON command, not a shell")
	}
	want := []string{"/usr/local/bin/kodex-agent-runner", "image-tool-inventory", input.SpecSha256, input.ImmutableBuildSha256, input.RoleRuntimeContractSha256}
	if !reflect.DeepEqual(command, want) {
		t.Fatal("inventory probe loses or swaps independent immutable pins")
	}
	if !strings.HasPrefix(text, "# syntax=registry.invalid/frontend@sha256:"+input.FrontendSha256+"\nFROM registry.invalid/trusted@sha256:"+strings.Repeat("f", 64)+" AS trusted-runtime\n") {
		t.Fatal("inventory frontend and protected runner must retain independent pins")
	}
}

func TestNativeInventoryOutputCopiesOnlyManifestIntoFinalizedRootfs(t *testing.T) {
	input := &controlplanev1.RoleImageBuildInput{Dockerfile: "FROM registry.invalid/owner@sha256:" + strings.Repeat("a", 64)}
	text := string(dockerfile(input, "registry.invalid/frontend", "registry.invalid/trusted", "sha256:"+strings.Repeat("b", 64)))
	stage := strings.Split(text, "FROM kodex-final-rootfs AS kodex-tool-probe\n")
	if len(stage) != 2 {
		t.Fatal("native inventory stage must be unique")
	}
	lines := strings.Split(stage[1], "\n")
	if len(lines) != 5 || lines[2] != "FROM kodex-final-rootfs" || lines[3] != "COPY --from=kodex-tool-probe /tmp/kodex-tool-inventory.json /usr/share/kodex/tool-inventory.json" || lines[4] != "" {
		t.Fatal("final image must inherit finalized rootfs and copy only the canonical manifest")
	}
}

func TestNativeInventoryStageUsesExactOwnerAliasOrAssignedAlias(t *testing.T) {
	base := "registry.invalid/owner@sha256:" + strings.Repeat("a", 64)
	for _, test := range []struct {
		name, source, alias string
	}{
		{"assigned", "FROM " + base + "\nRUN true\n", "kodex-user-rootfs"},
		{"owner", "FROM " + base + " AS owner-final\nRUN true\n", "owner-final"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := &controlplanev1.RoleImageBuildInput{Dockerfile: test.source, BaseImageReference: "registry.invalid/owner", BaseImageDigest: "sha256:" + strings.Repeat("a", 64)}
			if !validOwnerDockerfile(input) {
				t.Fatal("canonical owner Dockerfile was rejected")
			}
			text := string(dockerfile(input, "registry.invalid/frontend", "registry.invalid/trusted", "sha256:"+strings.Repeat("b", 64)))
			if !strings.Contains(text, "FROM "+test.alias+" AS kodex-final-rootfs\n") {
				t.Fatal("inventory source differs from the exact finalized owner rootfs")
			}
		})
	}
}

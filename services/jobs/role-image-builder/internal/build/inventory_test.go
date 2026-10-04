package build

import (
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestFinalInventoryProbeIsTrustedReadOnlyAndAfterUserSource(t *testing.T) {
	digest := strings.Repeat("a", 64)
	input := &controlplanev1.RoleImageBuildInput{Dockerfile: "FROM registry.invalid/base@sha256:" + digest + " AS owner\nRUN printf owner\n", SpecSha256: digest, ImmutableBuildSha256: digest, RoleRuntimeContractSha256: digest, FrontendSha256: digest}
	text := string(dockerfile(input, "registry.invalid/frontend", "registry.invalid/runtime", "sha256:"+digest))
	for _, required := range []string{
		"FROM registry.invalid/base@sha256:" + digest + " AS owner\nRUN printf owner\nFROM owner AS kodex-final-rootfs",
		"FROM kodex-final-rootfs AS kodex-tool-probe\nUSER root",
		"RUN --network=none --mount=type=bind,from=kodex-final-rootfs,source=/,target=/image,readonly [\"/usr/local/bin/kodex-agent-runner\",\"image-tool-inventory\"",
		"FROM kodex-final-rootfs\nCOPY --from=kodex-tool-probe /tmp/kodex-tool-inventory.json /usr/share/kodex/tool-inventory.json",
	} {
		if !strings.Contains(text, required) {
			t.Fatal("trusted inventory stage contract missing")
		}
	}
	if strings.LastIndex(text, "RUN printf owner") > strings.Index(text, "FROM kodex-final-rootfs AS kodex-tool-probe") {
		t.Fatal("owner instructions execute after inventory")
	}
	probeStart := strings.Index(text, "FROM kodex-final-rootfs AS kodex-tool-probe")
	if strings.LastIndex(text[:probeStart], "COPY --from=trusted-runtime /usr/local/bin/kodex-agent-runner") < 0 {
		t.Fatal("native probe can execute an owner-supplied runner")
	}
	for _, alias := range []string{"trusted-runtime", "kodex-tool-probe", "kodex-final-rootfs"} {
		input.BaseImageReference, input.BaseImageDigest = "registry.invalid/base", "sha256:"+digest
		input.Dockerfile = "FROM registry.invalid/base@sha256:" + digest + " AS " + alias
		if validOwnerDockerfile(input) {
			t.Fatal("owner can shadow trusted stage")
		}
	}
}

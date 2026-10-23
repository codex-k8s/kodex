package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"
)

// testLegacyEmailGatewayClaim sends a trusted synthetic old-producer claim to a bounded caster
// test process, requiring a credential in that historical shape. The caller supplies only disposable
// fixture data and a trusted binary: child env/output are not redacted, and this helper sends no mail.
// testLegacyEmailGatewayClaim передаёт доверенный synthetic old-producer claim ограниченному caster
// test process, требуя credential исторической формы. Caller задаёт только disposable fixture data
// и доверенный binary: child env/output не очищаются; сама функция письмо не отправляет.
func testLegacyEmailGatewayClaim(t *testing.T, item map[string]any, kind string) {
	t.Helper()
	binary := os.Getenv("KODEX_EMAIL_CASTER_TEST_BINARY")
	if binary == "" || item["credential"] == nil {
		t.Fatal("legacy producer credential claim required")
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^TestLegacyEmailClaimCasterFixture$", "-test.timeout=18s")
	child.Env = append(os.Environ(), "KODEX_EMAIL_CLAIM_KIND="+kind, "KODEX_EMAIL_LEGACY_CLAIM=1")
	child.Stdin = bytes.NewReader(raw)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("old producer claim rejected: %v\n%s", err, output)
	}
}

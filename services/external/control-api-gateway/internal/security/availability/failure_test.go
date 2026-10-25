package availability

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
)

// TestStagePreservesMappingAndClosesFormatting rejects caller-shaped metadata and ensures nested decoration cannot replace the first native stage.
// TestStagePreservesMappingAndClosesFormatting отвергает метаданные вида caller data и запрещает вложенной обёртке заменять первый исходный этап.
func TestStagePreservesMappingAndClosesFormatting(t *testing.T) {
	err := At("ticket_ledger_read", fmt.Errorf("private-credential: %w", browserstate.ErrUnavailable))
	err = At("auth_family_read", err)
	stage, class := Describe(err)
	if stage != "ticket_ledger_read" || class != "unavailable" || !errors.Is(err, browserstate.ErrUnavailable) {
		t.Fatal("stage changed public semantics")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		if strings.Contains(fmt.Sprintf(format, err), "private-credential") {
			t.Fatal("diagnostic formatting leaked")
		}
	}
	stage, class = Describe(Fixed("private-credential", "private-credential", browserstate.ErrUnavailable))
	if stage != "unknown" || class != "unknown" || At("auth_family_read", nil) != nil {
		t.Fatal("metadata normalization failed")
	}
}

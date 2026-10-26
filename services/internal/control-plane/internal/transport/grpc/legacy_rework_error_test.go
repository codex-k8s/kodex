package grpc

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestLegacyReworkTransportErrors preserves wrapped conflict identity while exposing only the exact closed reason, never joined diagnostic text.
// TestLegacyReworkTransportErrors сохраняет conflict identity обёрнутой ошибки, выдавая только точный закрытый reason без joined diagnostic text.
func TestLegacyReworkTransportErrors(t *testing.T) {
	for _, test := range []struct {
		err    error
		reason string
	}{
		{errs.ErrLegacyReworkScopeUnsupported, "LEGACY_REWORK_SCOPE_UNSUPPORTED"},
		{errs.ErrLegacyReworkWrite, "LEGACY_REWORK_WRITE_REQUIRES_RECONCILIATION"},
		{errs.ErrLegacyReworkParentBusy, "LEGACY_REWORK_PARENT_BUSY"},
		{errs.ErrLegacyReworkDeliveryBlocked, "LEGACY_REWORK_DELIVERY_BLOCKED"},
		{errs.ErrLegacyReworkReadLimit, "LEGACY_REWORK_READ_LIMIT"},
	} {
		t.Run(test.reason, func(t *testing.T) {
			wrapped := fmt.Errorf("private-input-canary: %w", test.err)
			mapped := transportError(wrapped)
			if !errors.Is(wrapped, errs.ErrConflict) || status.Code(mapped) != codes.FailedPrecondition || errorInfoReason(mapped) != test.reason {
				t.Fatal("owner rework failure lost its domain identity or typed public boundary")
			}
			if strings.Contains(fmt.Sprintf("%v %+v %#v %s %q", mapped, mapped, mapped, mapped, mapped), "private-input-canary") {
				t.Fatal("owner rework transport leaked joined input")
			}
		})
	}
}

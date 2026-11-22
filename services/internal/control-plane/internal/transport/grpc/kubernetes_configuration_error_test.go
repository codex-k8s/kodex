package grpc

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestKubernetesConfigurationPlanTransportError retains a fixed unsupported reason without
// exposing owner/grant diagnostic text or suggesting that Kubernetes runtime is unsupported.
// TestKubernetesConfigurationPlanTransportError сохраняет fixed unsupported reason без
// owner/grant diagnostic text и без объявления Kubernetes runtime неподдерживаемым.
func TestKubernetesConfigurationPlanTransportError(t *testing.T) {
	wrapped := fmt.Errorf("private-input-canary: %w", errs.ErrKubernetesConfigurationPlanUnsupported)
	mapped := transportError(wrapped)
	if !errors.Is(wrapped, errs.ErrConflict) || status.Code(mapped) != codes.FailedPrecondition || errorInfoReason(mapped) != managedkubernetes.ConfigurationPlanUnsupportedReason || strings.Contains(mapped.Error(), "private-input-canary") {
		t.Fatal("configuration-plan error crossed its closed boundary")
	}
}

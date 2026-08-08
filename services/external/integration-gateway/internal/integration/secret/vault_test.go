package secret

import (
	"errors"
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/secretstore"
)

func TestVaultDestroyReadbackIsCrashRepeatableAndFailClosed(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		observed  uint64
		destroyed bool
		readErr   error
		wantErr   bool
	}{
		"exact":         {observed: 7, destroyed: true},
		"already_gone":  {readErr: secretstore.ErrNotFound},
		"mismatch":      {observed: 8, destroyed: true, wantErr: true},
		"not_destroyed": {observed: 7, wantErr: true},
		"unknown":       {readErr: errors.New("Vault unavailable"), wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			err := confirmVaultDestroy(7, test.observed, test.destroyed, test.readErr)
			if (err != nil) != test.wantErr {
				t.Fatalf("unexpected destroy readback result: %v", err)
			}
		})
	}
}

package authority

import (
	"errors"
	"testing"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
)

func TestResolveProjectID(t *testing.T) {
	t.Parallel()

	const (
		first  = "bf51b17a-94d2-4f7e-a7f4-1b014fceec0d"
		second = "bcda470d-95dd-4839-bd59-55e1032d61f7"
	)
	tests := []struct {
		name       string
		required   bool
		credential string
		locator    string
		want       string
		wantErr    error
	}{
		{name: "credential scope", required: true, credential: first, want: first},
		{name: "browser locator", required: true, locator: first, want: first},
		{name: "matching scopes", required: true, credential: first, locator: first, want: first},
		{name: "scope mismatch", required: true, credential: first, locator: second, wantErr: errs.ErrPermissionDenied},
		{name: "missing scope", required: true, wantErr: errs.ErrPermissionDenied},
		{name: "invalid locator", required: true, locator: "invalid", wantErr: errs.ErrInvalidInput},
		{name: "global operation", want: ""},
		{name: "locator on global operation", locator: first, wantErr: errs.ErrPermissionDenied},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveProjectID(test.required, test.credential, test.locator)
			if !errors.Is(err, test.wantErr) || got != test.want {
				t.Fatalf("resolveProjectID() = (%q, %v), want (%q, %v)", got, err, test.want, test.wantErr)
			}
		})
	}
}

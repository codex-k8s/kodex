package roleimage

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestStandardCatalogTemplateUsesExactServerPins(t *testing.T) {
	environment := validEnvironmentWithKey("standard", true, true)
	catalog, err := NewCatalog([]Environment{environment})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := catalog.Resolve(entity.RoleEnvironmentSelection{EnvironmentKey: "standard"})
	if err != nil {
		t.Fatal(err)
	}
	want := "FROM " + environment.Input.BaseImageReference + "@" + environment.Input.BaseImageDigest + "\n"
	if resolved.Dockerfile != want || resolved.EnvironmentKey != "standard" || resolved.BaseImageDigest != environment.Input.BaseImageDigest ||
		resolved.ContextSHA256 != environment.Input.ContextSHA256 || resolved.BuilderSHA256 != environment.Input.BuilderSHA256 ||
		resolved.FrontendSHA256 != environment.Input.FrontendSHA256 || resolved.ToolchainSHA256 != environment.Input.ToolchainSHA256 {
		t.Fatal("catalog template lost exact server-owned supply-chain pins")
	}
	for _, dockerfile := range []string{want + "RUN echo synthetic\n", "FROM foreign.invalid/image@" + environment.Input.BaseImageDigest + "\n"} {
		_, err := catalog.Resolve(entity.RoleEnvironmentSelection{EnvironmentKey: "standard", Dockerfile: dockerfile})
		if (dockerfile != want+"RUN echo synthetic\n") != errors.Is(err, errs.ErrInvalid) {
			t.Fatal("explicit Dockerfile validation differs from the exact catalog base")
		}
	}
	if _, err := catalog.Resolve(entity.RoleEnvironmentSelection{EnvironmentKey: "guessed-toolchain"}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("unknown environment key gained a fallback template")
	}
}

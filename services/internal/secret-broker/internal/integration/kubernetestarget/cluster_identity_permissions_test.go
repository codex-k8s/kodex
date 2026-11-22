package kubernetestarget

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestIdentityHealthRequiresExplicitGlobalNamespacePermission proves that ordinary Pod grants
// do not imply the current strict cluster identity prerequisite. A selected-identity 403 never
// becomes authenticated health, a fabricated UID, another credential or a resource permission.
// TestIdentityHealthRequiresExplicitGlobalNamespacePermission доказывает, что обычные Pod grants
// не подтверждают strict cluster identity prerequisite. Selected-identity403 не становится
// authenticated health, fabricated UID, другой credential либо resource permission.
func TestIdentityHealthRequiresExplicitGlobalNamespacePermission(t *testing.T) {
	reviews := 0
	credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == "/api/v1/namespaces/kube-system" {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusForbidden)
			_, _ = writer.Write([]byte(`{"kind":"Status","message":"synthetic-private-provider-body"}`))
			return
		}
		reviews++
		writer.WriteHeader(http.StatusForbidden)
	}))
	digest, err := credential.ObserveIdentity(context.Background())
	if err == nil || digest != "" || calls.Load() != 1 || reviews != 0 || strings.Contains(err.Error(), "synthetic-private-provider-body") {
		t.Fatal("missing kube-system namespace permission inferred identity or leaked provider body")
	}
}

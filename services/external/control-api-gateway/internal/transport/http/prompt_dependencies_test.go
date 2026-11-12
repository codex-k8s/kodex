package httptransport

import (
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// TestPromptDependencyViewsRejectContradictoryEvidence checks the closed transport boundary independently of the renderer.
// TestPromptDependencyViewsRejectContradictoryEvidence проверяет закрытую transport-границу независимо от renderer.
func TestPromptDependencyViewsRejectContradictoryEvidence(t *testing.T) {
	valid := &cp.PromptTemplateDependency{TemplateDigest: strings.Repeat("a", 64), Path: "task", Stability: "TURN_LIVE", Sensitivity: "PRIVATE", TrustScope: "DATA_ONLY"}
	if _, ok := promptDependencyViews([]*cp.PromptTemplateDependency{valid}); !ok {
		t.Fatal("valid live dependency rejected")
	}
	for _, mutate := range []func(*cp.PromptTemplateDependency){
		func(v *cp.PromptTemplateDependency) { v.Stability = "CACHEABLE" },
		func(v *cp.PromptTemplateDependency) { v.Stability = "VERSION_PINNED" },
		func(v *cp.PromptTemplateDependency) { v.SourceDigest = strings.Repeat("b", 64) },
		func(v *cp.PromptTemplateDependency) { v.TrustScope = "OWNER_TEMPLATE" },
		func(v *cp.PromptTemplateDependency) { v.Sensitivity = "PUBLIC" },
		func(v *cp.PromptTemplateDependency) { v.Path = "$literal" },
	} {
		changed := proto.Clone(valid).(*cp.PromptTemplateDependency)
		mutate(changed)
		if _, ok := promptDependencyViews([]*cp.PromptTemplateDependency{changed}); ok {
			t.Fatal("contradictory dependency accepted")
		}
	}
	if _, ok := promptDependencyViews([]*cp.PromptTemplateDependency{valid, valid}); ok {
		t.Fatal("duplicate dependency accepted")
	}
	if _, ok := promptDependencyViews([]*cp.PromptTemplateDependency{nil}); ok {
		t.Fatal("nil dependency accepted")
	}
}

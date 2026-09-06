package grpc

import (
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
)

func TestPromptSectionPreservesTypedUserProvenance(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, kind := range []string{"WORKFLOW_CONTEXT", "AUTOMATION_TASK"} {
		value, err := castPromptSection(promptservice.Section{Source: "USER_TEMPLATE", UserKind: kind, TemplateRef: "mrev_abcdefgh", TemplateDigest: digest, Content: "user text"}, "ins_base", digest)
		if err != nil || value.GetUserKind().String() != "PROMPT_USER_SECTION_KIND_"+kind || value.GetTemplateRef() != "mrev_abcdefgh" || value.GetTemplateDigest() != digest {
			t.Fatalf("lost user provenance: %v", err)
		}
	}
	base, err := castPromptSection(promptservice.Section{Source: "USER_TEMPLATE", Content: "base"}, "ins_base", digest)
	if err != nil || base.UserKind != controlplanev1.PromptUserSectionKind_PROMPT_USER_SECTION_KIND_BASE_TEMPLATE || base.TemplateRef != "ins_base" {
		t.Fatal("lost base provenance")
	}
	for _, section := range []promptservice.Section{
		{Source: "PLATFORM", Slot: promptservice.SlotInput, UserKind: "WORKFLOW_CONTEXT"},
		{Source: "USER_TEMPLATE", UserKind: "UNKNOWN"},
		{Source: "USER_TEMPLATE", UserKind: "WORKFLOW_CONTEXT", TemplateRef: "mrev_abcdefgh", TemplateDigest: strings.ToUpper(digest)},
		{Source: "USER_TEMPLATE", UserKind: "AUTOMATION_TASK", TemplateDigest: digest},
		{Source: "USER_TEMPLATE", Slot: promptservice.SlotInput},
	} {
		if _, err := castPromptSection(section, "ins_base", digest); err == nil {
			t.Fatal("malformed user provenance accepted")
		}
	}
}

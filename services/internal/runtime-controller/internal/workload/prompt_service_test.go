package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"k8s.io/client-go/kubernetes/fake"
)

func setSemanticPrompt(revision *controlplanev1.RuntimeRevisionSnapshot) {
	slots := []string{"PURPOSE", "INPUT", "CONSTRAINTS", "EFFECTIVE_CAPABILITIES", "FILES", "TOOLS", "INTEGRATIONS"}
	var capabilities []string
	for _, capability := range revision.Capabilities {
		capabilities = append(capabilities, capability.Key)
	}
	sort.Strings(capabilities)
	var sections []runtimecontract.PromptServiceSection
	for _, slot := range slots {
		content := ""
		if slot == "EFFECTIVE_CAPABILITIES" {
			content = strings.Join(capabilities, "\n")
		}
		sections = append(sections, runtimecontract.PromptServiceSection{Source: "PLATFORM", Slot: slot, Content: content})
	}
	raw, _ := json.Marshal(runtimecontract.PromptServiceEnvelope{Revision: runtimecontract.PromptServiceRevision, Locale: "en", Sections: sections})
	revision.Instructions = string(raw)
	service, _ := json.Marshal(struct {
		Revision, Locale, Kind string
		Slots                  []string
	}{runtimecontract.PromptServiceRevision, "en", "AGENT", slots})
	digest := sha256.Sum256(service)
	revision.PromptServiceTemplateRevision = runtimecontract.PromptServiceRevision
	revision.PromptServiceTemplateDigest = hex.EncodeToString(digest[:])
	revision.PromptTargetKind = "AGENT"
}

func TestWarmAndTurnPromptProvenanceSurvivesControllerBinding(t *testing.T) {
	manager := newTestManager(t, fake.NewSimpleClientset())
	for _, warm := range []bool{false, true} {
		execution := testExecution(false)
		revision := execution.Revision
		if warm {
			revision = testWarmRevision()
		}
		setSemanticPrompt(revision)
		var input runtimecontract.RunnerInput
		var err error
		if warm {
			sealTestWarmRevision(revision)
			input, _, err = manager.BuildWarmInput(revision)
		} else {
			sealTestTurnExecution(execution)
			input, _, err = manager.BuildTurnInput(execution)
		}
		if err != nil {
			t.Fatal(err)
		}
		if input.PromptServiceTemplateRevision != revision.PromptServiceTemplateRevision || input.PromptServiceTemplateDigest != revision.PromptServiceTemplateDigest || input.PromptTargetKind != revision.PromptTargetKind {
			t.Fatal("prompt provenance mapping lost")
		}
		if _, err := runtimecontract.DecodePromptService(input); err != nil {
			t.Fatal(err)
		}
		revision.PromptTargetKind = "WORKFLOW_STAGE"
		if warm {
			_, _, err = manager.BuildWarmInput(revision)
		} else {
			_, _, err = manager.BuildTurnInput(execution)
		}
		if err == nil {
			t.Fatal("changed prompt provenance bypassed immutable binding")
		}
	}
}

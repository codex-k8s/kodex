package prompt

import (
	"strings"
	"testing"
)

func TestContinuationDiffPinsAllChangesAndTurnIdentity(t *testing.T) {
	previous, current := map[string][]RuntimeDescriptor{}, map[string][]RuntimeDescriptor{}
	for _, component := range runtimeComponentOrder {
		previous[component] = []RuntimeDescriptor{}
		current[component] = []RuntimeDescriptor{}
	}
	previous["MODEL"] = []RuntimeDescriptor{{Value: "old-model"}}
	current["MODEL"] = []RuntimeDescriptor{{Value: "new-model"}}
	current["REASONING"] = []RuntimeDescriptor{{Value: "high"}}
	identity := RuntimeDiff{PreviousRevisionRef: "rrev_previous", CurrentRevisionRef: "rrev_current", SessionRef: "ses_example", TurnRef: "turn_example", Attempt: 1}
	diff, err := CompareRuntimeContexts(previous, current, identity)
	if err != nil || len(diff.Changes) != 2 || diff.Changes[0].Component != "MODEL" || diff.Changes[1].Component != "REASONING" || !validDigest(diff.Digest) {
		t.Fatalf("invalid typed diff: %#v err=%v", diff, err)
	}
	identity.Attempt++
	changed, err := CompareRuntimeContexts(previous, current, identity)
	if err != nil || changed.Digest == diff.Digest {
		t.Fatal("attempt not bound")
	}
	current["CREDENTIAL"] = []RuntimeDescriptor{{Value: "forbidden"}}
	if _, err := CompareRuntimeContexts(previous, current, identity); err == nil {
		t.Fatal("unknown/private component accepted")
	}
	delete(current, "CREDENTIAL")
	current["IMAGE"] = []RuntimeDescriptor{{Digest: strings.Repeat("X", 64)}}
	if _, err := CompareRuntimeContexts(previous, current, identity); err == nil {
		t.Fatal("malformed digest accepted")
	}
}

func TestProspectiveDiffRejectsFabricatedIdentityAndTampering(t *testing.T) {
	previous, current := map[string][]RuntimeDescriptor{}, map[string][]RuntimeDescriptor{}
	for _, component := range runtimeComponentOrder {
		previous[component], current[component] = nil, nil
	}
	current["MODEL"] = []RuntimeDescriptor{{Value: "selected-model"}}
	identity := RuntimeDiff{PreviousRevisionRef: "rrev_previous", SessionRef: "ses_example"}
	diff, err := CompareProspectiveRuntimeContexts(previous, current, identity)
	if err != nil || ValidateRuntimeDiff(diff) != nil {
		t.Fatalf("valid prospective diff rejected: %v", err)
	}
	for _, mutate := range []struct {
		name  string
		apply func(*RuntimeDiff)
	}{
		{"future revision", func(v *RuntimeDiff) { v.CurrentRevisionRef = "rrev_fabricated" }},
		{"future turn", func(v *RuntimeDiff) { v.TurnRef = "turn_fabricated" }},
		{"future attempt", func(v *RuntimeDiff) { v.Attempt = 1 }},
		{"tampered content", func(v *RuntimeDiff) { v.Changes[0].Current = []RuntimeDescriptor{{Value: "another-model"}} }},
		{"unknown action", func(v *RuntimeDiff) { v.Changes[0].Action = "EXECUTE" }},
		{"unknown component", func(v *RuntimeDiff) { v.Changes[0].Component = "CREDENTIAL" }},
		{"duplicate component", func(v *RuntimeDiff) { v.Changes = append(v.Changes, v.Changes[0]) }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			copy := diff
			copy.Changes = append([]RuntimeChange{}, diff.Changes...)
			mutate.apply(&copy)
			if ValidateRuntimeDiff(copy) == nil {
				t.Fatal("invalid prospective diff accepted")
			}
		})
	}
}

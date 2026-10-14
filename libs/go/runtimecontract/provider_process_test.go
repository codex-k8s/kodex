package runtimecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

func processObservationInput() RunnerInput {
	return RunnerInput{OrganizationRef: "org_fixture123", ProjectRef: "prj_fixture123", RunRef: "run_fixture123", NodeRef: "nod_fixture123",
		SessionRef: "ses_fixture123", TurnRef: "trn_fixture123", Attempt: 1, RuntimeRevisionRef: "rev_fixture123", RuntimeRevisionVersion: 2,
		RuntimeRevisionDigest: strings.Repeat("a", 64), ImageReference: "pull.fixture.invalid/role@sha256:" + strings.Repeat("b", 64), ImageManifestDigest: "sha256:" + strings.Repeat("b", 64),
		InputDigest: strings.Repeat("c", 64), ExecutionBindingDigest: strings.Repeat("d", 64)}
}

func TestProviderProcessObservationCanonicalBinding(t *testing.T) {
	input := processObservationInput()
	value := BindProviderProcessObservation(input, "0.160.0")
	if !value.Matches(input) {
		t.Fatal("exact observation rejected")
	}
	raw, err := json.Marshal(value)
	var actual ProviderProcessObservation
	if err != nil || json.Unmarshal(raw, &actual) != nil || actual != value {
		t.Fatal("canonical typed wire lost binding")
	}
	for name, mutate := range map[string]func(*RunnerInput){
		"organization": func(v *RunnerInput) { v.OrganizationRef = "org_other123" }, "project": func(v *RunnerInput) { v.ProjectRef = "prj_other123" },
		"run": func(v *RunnerInput) { v.RunRef = "run_other123" }, "node": func(v *RunnerInput) { v.NodeRef = "nod_other123" },
		"session": func(v *RunnerInput) { v.SessionRef = "ses_other123" }, "turn": func(v *RunnerInput) { v.TurnRef = "trn_other123" }, "attempt": func(v *RunnerInput) { v.Attempt++ },
		"revisionRef": func(v *RunnerInput) { v.RuntimeRevisionRef = "rev_other123" }, "revisionVersion": func(v *RunnerInput) { v.RuntimeRevisionVersion++ },
		"revisionDigest": func(v *RunnerInput) { v.RuntimeRevisionDigest = strings.Repeat("e", 64) }, "image": func(v *RunnerInput) { v.ImageReference = "pull.fixture.invalid/other@" + v.ImageManifestDigest },
		"imageDigest": func(v *RunnerInput) { v.ImageManifestDigest = "sha256:" + strings.Repeat("e", 64) }, "input": func(v *RunnerInput) { v.InputDigest = strings.Repeat("e", 64) },
		"binding": func(v *RunnerInput) { v.ExecutionBindingDigest = strings.Repeat("e", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			other := input
			mutate(&other)
			if value.Matches(other) {
				t.Fatal("observation crossed execution binding")
			}
		})
	}
	for _, malformed := range []string{
		strings.Replace(string(raw), `"version":"0.160.0"`, `"version":"0.160.0","version":"0.160.0"`, 1),
		strings.Replace(string(raw), `"version":`, `"Version":`, 1), strings.Replace(string(raw), `"version":"0.160.0"`, `"version":null`, 1),
		strings.TrimSuffix(string(raw), "}") + `,"raw_user_agent":"PRIVATE_SENTINEL"}`, "null", "{}",
	} {
		if json.Unmarshal([]byte(malformed), &actual) == nil {
			t.Fatal("noncanonical observation accepted")
		}
	}
}

func TestProviderProcessVersionAndActivityClosedSchema(t *testing.T) {
	for _, version := range []string{"0.160.0", "1.2.3", "0.161.0-alpha.1", "0.161.0-rc.2"} {
		if !ValidProviderProcessVersion(version) {
			t.Fatal("canonical version rejected")
		}
	}
	for _, version := range []string{"", "unknown", "1", "01.160.0", "0.160.0+private", "0.160.0-dev.host", "0.160.0\n", strings.Repeat("1", 65)} {
		if ValidProviderProcessVersion(version) {
			t.Fatal("hostile/noncanonical version accepted")
		}
	}
	value := BindProviderProcessObservation(processObservationInput(), "0.160.0")
	if (RuntimeActivity{ProviderProcess: &value}).Validate() != nil {
		t.Fatal("typed activity rejected")
	}
	if (RuntimeActivity{ProviderProcess: &value, Message: &RuntimeAgentMessage{}}).Validate() == nil || (RuntimeActivity{}).Validate() == nil {
		t.Fatal("ambiguous activity accepted")
	}
}

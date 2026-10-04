package integrationpackage

import (
	"encoding/json"
	"testing"
)

func TestApprovalPolicyAllowedSetIsRequiredAndClosed(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	github := definitions["github"]
	for _, policy := range []string{"NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"} {
		capability, _ := github.Capability("github.pull_request.create")
		if !capability.AllowsApprovalPolicy(policy) {
			t.Fatal("collaborative policy is missing")
		}
	}
	for _, key := range []string{"github.pull_request.merge", "github.branch.delete", "github.repository.content.delete", "github.actions.run.cancel"} {
		capability, ok := github.Capability(key)
		if !ok || capability.AllowsApprovalPolicy("NONE") || !capability.AllowsApprovalPolicy("HUMAN_EACH_EFFECT") {
			t.Fatal("destructive approval boundary changed")
		}
	}
	for _, policies := range [][]string{nil, {}, {"UNKNOWN"}, {"NONE", "NONE"}, {"NONE"}} {
		candidate := github
		candidate.Spec.Capabilities = append([]Capability{}, github.Spec.Capabilities...)
		for i := range candidate.Spec.Capabilities {
			if candidate.Spec.Capabilities[i].Key == "github.pull_request.create" {
				candidate.Spec.Capabilities[i].AllowedApprovalPolicies = policies
			}
		}
		if validate(&candidate) == nil {
			t.Fatal("invalid allowed/default policy accepted")
		}
	}
	for _, key := range []string{"github.pull_request.merge", "github.issue.create"} {
		candidate := github
		candidate.Spec.Capabilities = append([]Capability{}, github.Spec.Capabilities...)
		for i := range candidate.Spec.Capabilities {
			if candidate.Spec.Capabilities[i].Key == key {
				candidate.Spec.Capabilities[i].AllowedApprovalPolicies = []string{"NONE", "HUMAN_EACH_EFFECT"}
			}
		}
		if validate(&candidate) == nil {
			t.Fatal("unapproved autonomous operation accepted")
		}
	}
}

func TestAutonomousReviewOnlyAllowsComment(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	github := definitions["github"]
	capability, _ := github.Capability("github.pull_request.review.create")
	for _, event := range []string{"COMMENT", "APPROVE", "REQUEST_CHANGES", "", "UNKNOWN"} {
		raw, _ := json.Marshal(map[string]string{"event": event})
		if (github.ValidateInvocationApprovalPolicy(capability, "NONE", raw) == nil) != (event == "COMMENT") {
			t.Fatal("autonomous review action boundary changed")
		}
	}
	if github.ValidateInvocationApprovalPolicy(capability, "HUMAN_EACH_EFFECT", []byte(`{"event":"APPROVE"}`)) != nil {
		t.Fatal("explicit owner approval policy rejected")
	}
}

func TestManagedRevisionCannotExpandAllowedPolicySet(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	github := definitions["github"]
	candidate := github
	candidate.Metadata.Origin = OriginUI
	candidate.Spec.Capabilities = append([]Capability{}, github.Spec.Capabilities...)
	for i := range candidate.Spec.Capabilities {
		if candidate.Spec.Capabilities[i].Key == "github.pull_request.create" {
			candidate.Spec.Capabilities[i].ApprovalPolicy = "NONE"
			candidate.Spec.Capabilities[i].AllowedApprovalPolicies = []string{"NONE"}
		}
	}
	raw, _ := json.Marshal(candidate)
	candidate, err = Parse(raw)
	if err != nil || ValidateExecutableRevision(candidate, github) != nil {
		t.Fatal("explicit approved narrowing rejected")
	}
	for i := range candidate.Spec.Capabilities {
		if candidate.Spec.Capabilities[i].Key == "github.repository.metadata.read" {
			candidate.Spec.Capabilities[i].AllowedApprovalPolicies = []string{"NONE", "HUMAN_EACH_EFFECT"}
		}
	}
	raw, _ = json.Marshal(candidate)
	if _, err = Parse(raw); err == nil {
		t.Fatal("read capability policy expansion accepted")
	}
}

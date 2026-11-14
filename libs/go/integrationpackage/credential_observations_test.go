package integrationpackage

import (
	"slices"
	"testing"
)

// TestCredentialObservationRequests pins requests to the closed executable package tuples and
// checks non-aliasing. Unregistered and modified write contracts must never request metadata rights.
// TestCredentialObservationRequests закрепляет requests за closed executable package tuples и
// проверяет отсутствие aliasing. Незарегистрированные и изменённые write contracts не запрашивают metadata rights.
func TestCredentialObservationRequests(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for key, definition := range definitions {
		if key == "gitlab" {
			definition = legacyGitLabPackage(t)
		}
		profiles, keys := CredentialObservationRequests(definition)
		if key == "grafana-organization" {
			if !slices.Equal(profiles, []string{CredentialObservationGrafanaPermissionsV1}) || !slices.Equal(keys, []string{"grafana.annotation.create", "grafana.annotation.update", "grafana.annotation.delete", "grafana.silence.create", "grafana.silence.update", "grafana.silence.delete"}) {
				t.Fatal("Grafana metadata request changed")
			}
			continue
		}
		if key == "sentry" || key == "sentry-self-hosted" {
			if !slices.Equal(profiles, []string{CredentialObservationSentryScopesV1}) || !slices.Equal(keys, []string{"sentry.issue.status.update", "sentry.issue.comment.create", "sentry.issue.assignment.update"}) {
				t.Fatal("Sentry metadata request changed")
			}
			continue
		}
		if key == "sentry-organization" || key == "sentry-self-hosted-organization" {
			if !slices.Equal(profiles, []string{CredentialObservationSentryScopesV1}) || !slices.Equal(keys, []string{"sentry.organization.release.create", "sentry.organization.release.update", "sentry.organization.alert.create", "sentry.organization.alert.update", "sentry.organization.alert.delete"}) {
				t.Fatal("Sentry organization metadata request changed")
			}
			continue
		}
		if key != "gitlab" {
			if len(profiles) != 0 || len(keys) != 0 {
				t.Fatal("unregistered definition obtained observations")
			}
			continue
		}
		if !slices.Equal(profiles, []string{CredentialObservationGitLabProjectWriteScopeV1}) || !slices.Equal(keys, []string{"gitlab.issue.create", "gitlab.branch.create", "gitlab.merge_request.create"}) {
			t.Fatal("GitLab request scope changed")
		}
		profiles[0], keys[0] = "mutated", "mutated"
		profiles, keys = CredentialObservationRequests(definition)
		if profiles[0] != CredentialObservationGitLabProjectWriteScopeV1 || keys[0] != "gitlab.issue.create" {
			t.Fatal("metadata requests share mutable state")
		}
		definition.Spec.Capabilities = append([]Capability(nil), definition.Spec.Capabilities...)
		for index := range definition.Spec.Capabilities {
			if CredentialObservationMayDeny(CredentialObservationGitLabProjectWriteScopeV1, definition.Spec.Capabilities[index].Key) {
				definition.Spec.Capabilities[index].ApprovalPolicy = "NONE"
			}
		}
		profiles, keys = CredentialObservationRequests(definition)
		if len(profiles) != 0 || len(keys) != 0 {
			t.Fatal("changed approval contract retained metadata request")
		}
	}
	if CredentialObservationMayDeny("UNKNOWN", "gitlab.issue.create") || CredentialObservationMayDeny(CredentialObservationGitLabProjectWriteScopeV1, "gitlab.issue.read") {
		t.Fatal("metadata profile accepts arbitrary capability")
	}
}

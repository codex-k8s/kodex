package integrationpackage

import "testing"

// TestSentryPackageBounds verifies canonical project scoping, separate Cloud/self-hosted origins and closed personal profiles without granting network access or expanding NONE to mutations.
// TestSentryPackageBounds проверяет точную область проекта, отдельные Cloud/self-hosted origins и закрытые personal profiles без сетевого доступа и расширения NONE на изменения.
func TestSentryPackageBounds(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"sentry", "sentry-self-hosted"} {
		definition := definitions[key]
		configuration := map[string]string{"base_url": "https://sentry.io", "organization_slug": "fixture-org", "project_slug": "fixture-project"}
		if key == "sentry-self-hosted" {
			configuration["base_url"] = "https://sentry.example.invalid"
		}
		if definition.ValidateConfiguration(configuration) != nil || len(definition.Spec.Capabilities) != 13 {
			t.Fatal("Sentry package cannot execute the documented vertical")
		}
		for _, capability := range definition.Spec.Capabilities {
			scope, err := capability.ResourceScopeValues(configuration)
			if err != nil || len(scope) != 3 || scope["organization_slug"] != "fixture-org" || scope["project_slug"] != "fixture-project" {
				t.Fatal("Sentry scope lost owner coordinates")
			}
			profile := CredentialResourcePermissionProfile(definition, capability)
			if profile == "" {
				t.Fatal("Sentry operation lacks mandatory personal consumer profile")
			}
			if capability.Risk != "READ" && (capability.AllowsApprovalPolicy("NONE") || !capability.AllowsApprovalPolicy("HUMAN_SCOPED") || !capability.AllowsApprovalPolicy("HUMAN_EACH_EFFECT")) {
				t.Fatal("Sentry mutation approval contract widened")
			}
		}
		configuration["project_slug"] = "../foreign"
		if definition.ValidateConfiguration(configuration) == nil {
			t.Fatal("Sentry traversal accepted")
		}
		configuration["project_slug"] = "fixture-project"
		configuration["base_url"] = "https://foreign.example.invalid"
		if (definition.ValidateConfiguration(configuration) == nil) != (key == "sentry-self-hosted") {
			t.Fatal("Cloud origin bypass or self-hosted origin refusal")
		}
	}
	for _, value := range []string{"", "..", "x/y", "%2f", "x?query=1", "x#fragment"} {
		if ValidSentrySlug(value) {
			t.Fatal("Sentry unsafe slug accepted")
		}
	}
}

// TestSentryOrganizationScopeIsExplicit ensures full-organization mutation authority cannot be selected by adding input/configuration fields to an existing project connection.
// TestSentryOrganizationScopeIsExplicit гарантирует невозможность выбора full-organization mutation authority добавлением input/configuration fields к существующему project connection.
func TestSentryOrganizationScopeIsExplicit(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"sentry-organization", "sentry-self-hosted-organization"} {
		definition := definitions[key]
		origin := "https://sentry.io"
		if key == "sentry-self-hosted-organization" {
			origin = "https://sentry.example.invalid"
		}
		config := map[string]string{"base_url": origin, "organization_slug": "fixture-org"}
		if definition.ValidateConfiguration(config) != nil || len(definition.Spec.Capabilities) != 13 {
			t.Fatal("explicit organization definition is invalid")
		}
		for _, capability := range definition.Spec.Capabilities {
			scope, err := capability.ResourceScopeValues(config)
			if err != nil || len(scope) != 2 {
				t.Fatal("organization scope inherited project coordinates")
			}
			if capability.Risk != "READ" && (len(capability.AllowedApprovalPolicies) != 1 || capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" || capability.AllowsApprovalPolicy("NONE") || capability.AllowsApprovalPolicy("HUMAN_SCOPED")) {
				t.Fatal("shared object mutation bypassed EACH gate")
			}
		}
		config["project_slug"] = "fixture-project"
		if definition.ValidateConfiguration(config) == nil {
			t.Fatal("project field silently changed organization authority")
		}
	}
	project := definitions["sentry"]
	for _, operation := range []string{"sentry.organization.release.create", "sentry.organization.release.update", "sentry.organization.alert.create", "sentry.organization.alert.delete"} {
		if _, exists := project.Capability(operation); exists {
			t.Fatal("project definition gained organization mutation")
		}
	}
}

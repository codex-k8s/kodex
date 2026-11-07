package integrationpackage

import "testing"

// TestCredentialResourcePermissionProfile excludes Cloud, other operations and downgraded policy.
// TestCredentialResourcePermissionProfile исключает Cloud, чужие операции и пониженную policy.
func TestCredentialResourcePermissionProfile(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"confluence-server-dc", "confluence", "jira-server-dc", "gitlab"} {
		definition := definitions[key]
		for _, capability := range definition.Spec.Capabilities {
			want := ""
			if contract, exists := credentialObjectReadContract(key, capability.Key); exists {
				want = contract.Profile
			}
			if key == "confluence-server-dc" && capability.Key == "confluence.page.update" {
				want = CredentialResourcePermissionConfluencePageUpdateV1
			}
			if key == "jira-server-dc" {
				switch capability.Key {
				case "jira.issue.create":
					want = CredentialResourcePermissionJiraIssueCreateV1
				case "jira.issue.comment.write":
					want = CredentialResourcePermissionJiraCommentWriteV1
				case "jira.issue.transition.apply":
					want = CredentialResourcePermissionJiraTransitionApplyV1
				}
			}
			if key == "gitlab" {
				switch capability.Key {
				case "gitlab.issue.create":
					want = CredentialResourcePermissionGitLabIssueCreateV1
				case "gitlab.branch.create":
					want = CredentialResourcePermissionGitLabBranchCreateV1
				case "gitlab.merge_request.create":
					want = CredentialResourcePermissionGitLabMergeRequestCreateV1
				}
			}
			if got := CredentialResourcePermissionProfile(definition, capability); got != want {
				t.Fatalf("profile mismatch for %s/%s", key, capability.Key)
			}
			if want != "" {
				if capability.Risk == "READ" {
					capability.Risk = "WRITE"
				} else {
					capability.Risk = "READ"
				}
				if CredentialResourcePermissionProfile(definition, capability) != "" {
					t.Fatal("downgraded capability retained the object-check profile")
				}
			}
		}
	}
}

// TestCredentialPermissionJSON rejects ambiguous permission observations without changing the
// package/schema parser's case-sensitive contract or making a provider authorization decision.
// TestCredentialPermissionJSON отклоняет неоднозначные наблюдения прав без изменения
// регистрозависимого package/schema parser и без решения об авторизации провайдера.
func TestCredentialPermissionJSON(t *testing.T) {
	for _, input := range []string{
		`{"permissions":{"ADD_COMMENTS":{"havePermission":false,"havePermission":true}}}`,
		`{"operations":[{"operation":"read","Operation":"update","targetType":"page"}]}`,
		`{"permissions":{},"Permissions":{}}`, `{}`, `null`,
		`{"status":"current","ſtatus":"other"}`, `{"key":1,"Key":2}`,
	} {
		err := ValidateCredentialPermissionJSON([]byte(input))
		if input == `{}` || input == `null` {
			if err != nil {
				t.Fatal("shape-free uniqueness validation rejected an unambiguous value")
			}
		} else if err == nil {
			t.Fatal("ambiguous permission JSON was accepted")
		}
	}
	if ValidateCredentialPermissionJSON([]byte(`{"permissions":{"ADD_COMMENTS":{"key":"ADD_COMMENTS","type":"PROJECT","havePermission":true}}}`)) != nil {
		t.Fatal("canonical permission JSON was rejected")
	}
}

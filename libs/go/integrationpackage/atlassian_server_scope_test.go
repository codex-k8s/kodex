package integrationpackage

import "testing"

// TestAtlassianInstanceTargets verifies explicit instance opt-in, pinned legacy locators and one approval per precise write.
// TestAtlassianInstanceTargets проверяет явное включение инстанса, прежние координаты и отдельное подтверждение точной записи.
func TestAtlassianInstanceTargets(t *testing.T) {
	shipped, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		definition, capability, policy, raw string
		selector, expected                  string
		valid                               bool
	}{
		{"jira-server-dc", "jira.issue.read", "NONE", `{"issue_key":"DEMO_2-10"}`, "project_key", "DEMO_2", true},
		{"jira-server-dc", "jira.issue.search", "NONE", `{"limit":5}`, "project_key", "", true},
		{"jira-server-dc", "jira.issue.create", "HUMAN_EACH_EFFECT", `{"summary":"New task","project_key":"DEMO","issue_type":"Task"}`, "project_key", "DEMO", true},
		{"jira-server-dc", "jira.issue.create", "HUMAN_EACH_EFFECT", `{"summary":"New task","project_key":"DEMO"}`, "", "", false},
		{"jira-server-dc", "jira.issue.comment.write", "NONE", `{"issue_key":"DEMO-10","body":"Comment"}`, "", "", false},
		{"confluence-server-dc", "confluence.page.read", "NONE", `{"page_id":"101"}`, "space_key", "", true},
		{"confluence-server-dc", "confluence.page.search", "NONE", `{"space_key":"~demo"}`, "space_key", "~demo", true},
		{"confluence-server-dc", "confluence.page.update", "HUMAN_EACH_EFFECT", `{"page_id":"101","expected_version":2,"body":"Body"}`, "", "", false},
		{"confluence-server-dc", "confluence.page.update", "HUMAN_SCOPED", `{"page_id":"101","space_key":"DEMO","expected_version":2,"body":"Body"}`, "", "", false},
		{"confluence-server-dc", "confluence.page.update", "HUMAN_EACH_EFFECT", `{"page_id":"101","space_key":"DEMO","expected_version":2,"body":"Body"}`, "space_key", "DEMO", true},
		{"confluence-server-dc", "confluence.space.read", "NONE", `{"space_key":".."}`, "", "", false},
	} {
		t.Run(item.capability+"/"+item.raw+"/"+item.policy, func(t *testing.T) {
			c, ok := shipped[item.definition].Capability(item.capability)
			if !ok {
				t.Fatal("operation missing")
			}
			config := map[string]string{"base_url": "https://api.example.invalid", "context_path": "/", "auth_scheme": "BEARER"}
			if shipped[item.definition].ValidateConfiguration(config) == nil {
				t.Fatal("missing selector enabled instance access implicitly")
			}
			config["scope_kind"] = "INSTANCE"
			if err := shipped[item.definition].ValidateConfiguration(config); err != nil {
				t.Fatal(err)
			}
			scope, err := c.ResourceScopeValues(config)
			if err != nil || scope["scope_kind"] != "INSTANCE" || scope["context_path"] != "/" || len(scope) != 3 {
				t.Fatalf("unexpected scope %v: %v", scope, err)
			}
			canonical, err := c.ValidateInput([]byte(item.raw))
			if err != nil {
				t.Fatal(err)
			}
			target, err := c.ResolveAtlassianServerTarget(item.policy, config, canonical)
			if (err == nil) != item.valid || (item.valid && target[item.selector] != item.expected) {
				t.Fatalf("target %v, err %v", target, err)
			}
			if config["project_key"] != "" || config["space_key"] != "" {
				t.Fatal("resolved target mutated the original connection")
			}
		})
	}
}

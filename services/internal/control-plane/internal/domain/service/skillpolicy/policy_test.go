package skillpolicy

import (
	"strings"
	"testing"
)

func TestValidatePaths(t *testing.T) {
	for _, paths := range [][]string{{"SKILL.md"}, {"SKILL.md", "references/design.md", "assets/icon.png"}} {
		if err := ValidatePaths(paths); err != nil {
			t.Fatalf("valid paths rejected: %v", err)
		}
	}
	for _, paths := range [][]string{nil, {"notes.md"}, {"skill.md"}, {"SKILL.md", "SKILL.md"}, {"SKILL.md", "../secret.txt"}, {"SKILL.md", "/secret.txt"}, {"SKILL.md", "a/../secret.txt"}, {"SKILL.md", ".git/config"}, {"SKILL.md", "a\\file.txt"}, {"SKILL.md", "run.sh"}, {"SKILL.md", "page.html"}, {"SKILL.md", "a.txt", "A.txt"}, {"SKILL.md", " folder/a.txt"}, {"SKILL.md", "a\x00.txt"}} {
		if ValidatePaths(paths) == nil {
			t.Fatalf("unsafe paths accepted: %q", paths)
		}
	}
}

func TestParseManifest(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		input := strings.ReplaceAll("---\nname: Example\ndescription: Safe documentation\n---\nRead these instructions.\n", "\n", newline)
		manifest, err := ParseManifest([]byte(input))
		if err != nil || manifest.Name != "Example" {
			t.Fatalf("valid manifest: %v", err)
		}
	}
	for _, input := range []string{
		"No frontmatter", "---\nname: Test\n---\nBody", "---\nname: Test\ndescription: Desc\n---\n  ",
		"---\nname: Test\nname: Override\ndescription: Desc\n---\nBody",
		"---\nname: [broken\ndescription: Desc\n---\nBody",
		"---\nname: Test\ndescription: Desc\npermissions: admin\n---\nBody",
		"---\nname: Test\ndescription: Desc\n---\nBody\x00",
		strings.Repeat("x", MaximumManifestBytes+1),
	} {
		if _, err := ParseManifest([]byte(input)); err == nil {
			t.Fatalf("invalid manifest accepted: %.100q", input)
		}
	}
}

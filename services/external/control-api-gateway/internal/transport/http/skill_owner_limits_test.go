package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

func TestSkillOwnerPathAndDescriptionLimits(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		valid      bool
	}{
		{"manifest", "SKILL.md", true},
		{"unicode-240-bytes", strings.Repeat("а", 118) + "a.md", true},
		{"unicode-241-bytes", strings.Repeat("а", 119) + ".md", false},
		{"nested", "references/data.json", true},
		{"wrong-case", "skill.md", false},
		{"dotfile", "references/.hidden.md", false},
		{"space", "references/ hidden.md", false},
		{"space-directory", "references /data.md", false},
		{"script", "scripts/run.sh", false},
		{"html", "references/page.html", false},
		{"archive", "data.zip", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if validSkillManifestPath(tc.path) != tc.valid {
				t.Fatal("path differs from owner constraints")
			}
		})
	}
	for _, count := range []int{2000, 2001} {
		var input generated.SkillBundleSpecification
		if err := json.Unmarshal([]byte(contextSkillSpec), &input); err != nil {
			t.Fatal(err)
		}
		input.Description = strings.Repeat("я", count)
		_, valid := skillSpecificationInput(input)
		if valid != (count == 2000) {
			t.Fatal("description must count Unicode characters")
		}
		skill, _, _ := contextFixtures()
		skill.DraftRevision.Description = input.Description
		_, valid = skillRevisionView(skill.DraftRevision)
		if valid != (count == 2000) {
			t.Fatal("response description differs from owner")
		}
	}
}

func TestSkillOwnerRejectsIncompleteOrAliasedFilesBeforeRPC(t *testing.T) {
	for _, paths := range [][]string{
		{}, {"references/notes.md"}, {"SKILL.md", "references/Notes.md", "references/notes.md"}, {"SKILL.md", "scripts/run.sh"},
	} {
		input := generated.SkillBundleSpecification{Name: "Fixture", Description: "Fixture", Files: []generated.SkillBundleFileInput{}}
		for _, path := range paths {
			input.Files = append(input.Files, generated.SkillBundleFileInput{Path: path, ArtifactRef: "art_fixture01", ArtifactRevision: 1})
		}
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		client := &contextRPCRecorder{}
		w := httptest.NewRecorder()
		contextHandler(client).ServeHTTP(w, managedTestRequest("PUT", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01", string(body)))
		if w.Code != 400 || client.request != nil {
			t.Fatalf("invalid file set forwarded: %d", w.Code)
		}
	}
}

func TestSkillOwnerResponseFileSizeLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sizes []int64
		valid bool
	}{
		{"per-file", []int64{32 << 20}, true},
		{"per-file-overflow", []int64{(32 << 20) + 1}, false},
		{"total", []int64{32 << 20, 32 << 20}, true},
		{"total-overflow", []int64{32 << 20, 32 << 20, 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			skill, _, _ := contextFixtures()
			revision := skill.DraftRevision
			file := revision.Files[0]
			revision.Files = nil
			for i, size := range tc.sizes {
				item := proto.Clone(file).(*controlplanev1.SkillBundleFile)
				item.SizeBytes = size
				if i > 0 {
					item.Path = strings.Repeat("a", i) + ".md"
				}
				revision.Files = append(revision.Files, item)
			}
			_, valid := skillRevisionView(revision)
			if valid != tc.valid {
				t.Fatal("response size differs from owner constraints")
			}
		})
	}
}

func TestSkillOwnerArtifactDigestUsesExactReceipt(t *testing.T) {
	skill, _, _ := contextFixtures()
	w := httptest.NewRecorder()
	writeSkillBundle(w, skill, skill.Ref, 200)
	var got generated.SkillBundle
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.DraftRevision == nil || got.DraftRevision.Files[0].Digest != "sha256:"+strings.Repeat("b", 64) {
		t.Fatal("artifact receipt digest was lost")
	}
	skill.DraftRevision.Files[0].Digest = strings.Repeat("b", 64)
	w = httptest.NewRecorder()
	writeSkillBundle(w, skill, skill.Ref, 200)
	if w.Code != 502 {
		t.Fatal("unqualified artifact digest accepted")
	}
}

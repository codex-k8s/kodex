package oidcverifier

import "testing"

// TestNormalizeGroupsBoundsAndSorts checks stable ordering, duplicate removal and rejection of unsafe or excessive claims.
// TestNormalizeGroupsBoundsAndSorts проверяет порядок, удаление повторов и отказ небезопасным или избыточным claims.
func TestNormalizeGroupsBoundsAndSorts(t *testing.T) {
	groups, err := normalizeGroups([]string{"operators", " auditors ", "operators"})
	if err != nil || len(groups) != 2 || groups[0] != "auditors" || groups[1] != "operators" {
		t.Fatalf("unexpected normalized groups: groups=%v err=%v", groups, err)
	}
	if _, err := normalizeGroups([]string{"operators\nadmins"}); err == nil {
		t.Fatal("control character in group was accepted")
	}
	tooMany := make([]string, maximumGroups+1)
	for index := range tooMany {
		tooMany[index] = "group"
	}
	if _, err := normalizeGroups(tooMany); err == nil {
		t.Fatal("unbounded group claim was accepted")
	}
}

package platform

import (
	"strings"
	"testing"
)

func TestProjectMembershipSearchIncludesVisibleProjectName(t *testing.T) {
	if !strings.Contains(queryProjectMembershipList, "project.name ILIKE") {
		t.Fatal("project membership search must include the visible project name")
	}
}

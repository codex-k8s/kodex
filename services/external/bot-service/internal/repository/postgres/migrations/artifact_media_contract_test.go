package migrations_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/libs/go/artifacttype"
)

func TestArtifactMediaTypeConstraintExactlyMatchesApplicationPolicy(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить каталог миграции")
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(currentFile), "000035_artifacts_vertical.sql"))
	if err != nil {
		t.Fatal(err)
	}
	startMarker := "media_type text not null check (media_type in ("
	start := strings.Index(string(body), startMarker)
	if start < 0 {
		t.Fatal("в 000035 не найден media_type constraint")
	}
	constraint := string(body)[start+len(startMarker):]
	end := strings.Index(constraint, ")),\n\tdeclared_media_type")
	if end < 0 {
		t.Fatal("в 000035 не найден конец media_type constraint")
	}
	matches := regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(constraint[:end], -1)
	databaseMediaTypes := make([]string, 0, len(matches))
	for _, match := range matches {
		databaseMediaTypes = append(databaseMediaTypes, match[1])
	}
	applicationMediaTypes := make([]string, 0, len(artifacttype.SupportedFormats()))
	for _, format := range artifacttype.SupportedFormats() {
		applicationMediaTypes = append(applicationMediaTypes, format.MediaType)
	}
	sort.Strings(databaseMediaTypes)
	sort.Strings(applicationMediaTypes)
	if !reflect.DeepEqual(databaseMediaTypes, applicationMediaTypes) {
		t.Fatalf("PostgreSQL и application MIME allowlists расходятся:\nPostgreSQL=%#v\napplication=%#v", databaseMediaTypes, applicationMediaTypes)
	}
}

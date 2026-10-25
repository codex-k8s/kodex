package platform

import (
	"strings"
	"testing"
)

// TestProjectAssistantGrantConnectionHeaderClosed accepts an exact nonempty connection header and
// rejects foreign identity, missing metadata, invalid version/digest or an unbound reference.
// TestProjectAssistantGrantConnectionHeaderClosed принимает точный непустой заголовок соединения и
// отклоняет чужую идентичность, отсутствующие метаданные, неверную версию или дайджест и
// несвязанный ref.
func TestProjectAssistantGrantConnectionHeaderClosed(t *testing.T) {
	valid := projectAssistantGrantConnectionHeader{ref: "int_owned", name: "Owned connection", version: 1, definitionKey: "github", definitionVersion: "6.0.0", definitionDigest: strings.Repeat("a", 64)}
	if !validProjectAssistantGrantConnectionHeader(valid, valid.ref) {
		t.Fatal("exact header rejected")
	}
	for _, field := range []string{"ref", "name", "version", "key", "revision", "digest"} {
		t.Run(field, func(t *testing.T) {
			value := valid
			switch field {
			case "ref":
				value.ref = "int_foreign"
			case "name":
				value.name = ""
			case "version":
				value.version = 0
			case "key":
				value.definitionKey = ""
			case "revision":
				value.definitionVersion = ""
			case "digest":
				value.definitionDigest = "PRIVATE_SENTINEL"
			}
			if validProjectAssistantGrantConnectionHeader(value, valid.ref) {
				t.Fatal("invalid header accepted")
			}
		})
	}
	if validProjectAssistantGrantConnectionHeader(valid, "") {
		t.Fatal("unbound header accepted")
	}
}

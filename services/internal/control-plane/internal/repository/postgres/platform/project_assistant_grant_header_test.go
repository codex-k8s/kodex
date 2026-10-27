package platform

import (
	"strings"
	"testing"
)

// TestAssistantGrantConnectionHeaderClosed accepts an exact nonempty connection header and
// rejects foreign identity, missing metadata, invalid version/digest or an unbound reference.
// TestAssistantGrantConnectionHeaderClosed принимает точный непустой заголовок соединения и
// отклоняет чужую идентичность, отсутствующие метаданные, неверную версию или дайджест и
// несвязанный ref.
func TestAssistantGrantConnectionHeaderClosed(t *testing.T) {
	valid := assistantGrantConnectionHeader{ref: "int_owned", name: "Owned connection", version: 1, definitionKey: "github", definitionVersion: "6.0.0", definitionDigest: strings.Repeat("a", 64)}
	if !validAssistantGrantConnectionHeader(valid, valid.ref) {
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
			if validAssistantGrantConnectionHeader(value, valid.ref) {
				t.Fatal("invalid header accepted")
			}
		})
	}
	if validAssistantGrantConnectionHeader(valid, "") {
		t.Fatal("unbound header accepted")
	}
}

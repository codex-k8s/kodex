package platform

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestWorkflowPDLCTextWriteBoundary checks that a permitted WRITE can be verified by the
// native text reader, including the maximum file with standard CRLF-wrapped base64.
// TestWorkflowPDLCTextWriteBoundary проверяет возможность подтвердить разрешённый WRITE
// штатным text reader, включая предельный файл с обычным CRLF-wrapped base64.
func TestWorkflowPDLCTextWriteBoundary(t *testing.T) {
	for _, mode := range []string{"empty", "utf8", "max-wrapped", "nul", "invalid-utf8", "oversize", "padding", "space", "tab"} {
		t.Run(mode, func(t *testing.T) {
			data := []byte("Требования<&>\n")
			switch mode {
			case "empty":
				data = nil
			case "max-wrapped":
				data = []byte(strings.Repeat("x", 256<<10))
			case "nul":
				data = []byte("Требования\x00\n")
			case "invalid-utf8":
				data = []byte{0xff, 0xfe}
			case "oversize":
				data = []byte(strings.Repeat("x", (256<<10)+1))
			}
			encoded := base64.StdEncoding.EncodeToString(data)
			if mode == "max-wrapped" {
				var lines []string
				for len(encoded) > 76 {
					lines, encoded = append(lines, encoded[:76]), encoded[76:]
				}
				encoded = strings.Join(append(lines, encoded), "\r\n")
			}
			if mode == "padding" {
				encoded = "AB=="
			} else if mode == "space" {
				encoded += " "
			} else if mode == "tab" {
				encoded += "\t"
			}
			_, actual, err := parseWorkflowPDLCFileWrite("github.repository.content.create", asJSON(map[string]any{
				"path": "docs/requirements.md", "branch": "feature/journal", "message": "Уточнить требования", "content_base64": encoded,
			}))
			valid := mode == "empty" || mode == "utf8" || mode == "max-wrapped"
			if (err == nil) != valid || valid && string(actual) != string(data) {
				t.Fatal("native text WRITE admission disagrees with readable byte contract", mode)
			}
		})
	}
}

// TestWorkflowPDLCResultHashContract uses independently fixed vectors also executed by
// PostgreSQL, including a UTF8 path whose HTML escaping differs between Go and SQL JSON.
// TestWorkflowPDLCResultHashContract использует независимо закреплённые vectors также для
// PostgreSQL, включая UTF8 path с разным HTML escaping в Go и SQL JSON.
func TestWorkflowPDLCResultHashContract(t *testing.T) {
	file := []string{"kodex.workflow-repository-file.v1", "document", "acme/repo", "feature/journal", "docs/требования<&>.md", "3", "erc_write0000", strings.Repeat("a", 64), "erc_head00000", strings.Repeat("b", 64), "erc_content00", strings.Repeat("c", 64)}
	comment := []string{"kodex.workflow-pr-comment.v1", "response", "acme/repo", "3", "erc_comment00", strings.Repeat("d", 64), strings.Repeat("e", 64)}
	if workflowPDLCResultDigest(file...) != "55b72a8e26754afc9bbaadc7d01d3c5f5e91271c24f504392938c6498234c7d1" ||
		workflowPDLCResultDigest(comment...) != "f97ba4bbc18ff97cd93adff14dd5cc8d6690d7b625cc962b86ffbf6bebb8362d" {
		t.Fatal("native result digest differs from the fixed Go/SQL contract")
	}
	for _, invalid := range [][]string{nil, file[:11], append(append([]string{}, comment...), "extra"), append([]string{"unknown"}, file[1:]...)} {
		if workflowPDLCResultDigest(invalid...) != "" {
			t.Fatal("result digest accepted an unknown domain or changed cardinality")
		}
	}
	file[4] = "docs/a\x00b.md"
	if workflowPDLCResultDigest(file...) != "" {
		t.Fatal("result digest admitted an ambiguous delimiter")
	}
}

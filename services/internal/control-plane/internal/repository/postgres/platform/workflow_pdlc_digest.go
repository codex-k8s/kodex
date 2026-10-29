package platform

import "strings"

// workflowPDLCResultDigest frames only the two new native result domains with UTF8 NUL
// delimiters, avoiding Go/SQL JSON escaping differences. Callers first validate every proof;
// this pure commitment does not establish authority or change historical result digests.
// workflowPDLCResultDigest соединяет поля только двух новых native result domains через UTF8
// NUL-разделитель без расхождений Go/SQL JSON escaping. Вызывающий сначала проверяет все proofs;
// чистый commitment не назначает полномочия и не меняет исторические result digests.
func workflowPDLCResultDigest(parts ...string) string {
	if len(parts) == 0 || !(parts[0] == "kodex.workflow-repository-file.v1" && len(parts) == 12 ||
		parts[0] == "kodex.workflow-pr-comment.v1" && len(parts) == 7) {
		return ""
	}
	for _, part := range parts {
		if strings.IndexByte(part, 0) >= 0 {
			return ""
		}
	}
	return workflowResultSHA([]byte(strings.Join(parts, "\x00")))
}

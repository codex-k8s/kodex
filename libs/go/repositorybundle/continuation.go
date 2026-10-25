package repositorybundle

import "strings"

// ValidContinuation accepts only absent delivery metadata, a typed oversized-task block or its explicit cancellation. A terminal Git receipt remains independent from presentation state.
// ValidContinuation принимает лишь отсутствие delivery metadata, typed oversized-task block либо его explicit cancellation. Terminal Git receipt остаётся независимой от presentation state.
func ValidContinuation(state, code, node string) bool {
	if state == "" {
		return code == "" && node == ""
	}
	if state != "BLOCKED" && state != "CANCELLED" || !strings.HasPrefix(node, "nod_") || len(node) < 12 || len(node) > 93 {
		return false
	}
	for _, r := range node[4:] {
		if r != '-' && r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return state == "BLOCKED" && (code == "CALLBACK_INPUT_TOO_LARGE" || code == "CONTINUATION_AUTHORITY_CHANGED") || state == "CANCELLED" && code == ""
}

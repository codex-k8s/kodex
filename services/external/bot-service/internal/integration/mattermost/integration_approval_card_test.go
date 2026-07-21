package mattermost

import "testing"

func TestExactApprovalCardPropsRejectsTamperAndUnknownData(t *testing.T) {
	expected := map[string]any{
		"matter_codex_delivery_id":      "apr_0123456789abcdef0123456789abcdef",
		"matter_codex_arguments_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	actual := map[string]any{
		"matter_codex_delivery_id":      "apr_0123456789abcdef0123456789abcdef",
		"matter_codex_arguments_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"from_bot":                      "true", "attachments": []any{},
	}
	if !exactApprovalCardProps(actual, expected) {
		t.Fatal("точные свойства approval-карточки не прошли readback")
	}
	actual["matter_codex_arguments_sha256"] = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if exactApprovalCardProps(actual, expected) {
		t.Fatal("подменённый arguments hash прошёл readback")
	}
	actual["matter_codex_arguments_sha256"] = expected["matter_codex_arguments_sha256"]
	actual["credential"] = "synthetic-value"
	if exactApprovalCardProps(actual, expected) {
		t.Fatal("неизвестное свойство карточки прошло fail-closed readback")
	}
}

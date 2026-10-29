package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

// workflowPDLCTestJSON encodes only local fixture data and rejects accidental unsupported values.
// workflowPDLCTestJSON кодирует только локальные fixture данные и отвергает неподдержанные значения.
func workflowPDLCTestJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal("encode PDLC SQL fixture")
	}
	return string(raw)
}

// TestWorkflowPDLCMigrationFunctions applies the in-progress forward migration to the public
// disposable fixture and proves hash/selector interoperability, ACL and forward-only replay.
// It does not attest complete effect/outcome guards, backup restoration, wire4 or live PDLC.
// TestWorkflowPDLCMigrationFunctions применяет дорабатываемую forward migration в публичной
// disposable fixture и доказывает совместимость hash/selector, ACL и forward-only replay.
// Не подтверждает полные effect/outcome guards, восстановление backup, wire4 либо живой PDLC.
func TestWorkflowPDLCMigrationFunctions(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261029000200); err != nil {
		t.Fatal("prepare PDLC exact predecessor", err)
	}
	metadata := func() []byte {
		t.Helper()
		var raw []byte
		if err := observer.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(oid::text,proowner::text,prosecdef,proconfig,proacl::text) ORDER BY oid)
FROM pg_proc WHERE oid IN ('control_plane.guard_workflow_effect_records()'::regprocedure,
'control_plane.guard_workflow_attempt_journal()'::regprocedure,'control_plane.workflow_public_attempt_results(uuid,uuid)'::regprocedure)`).Scan(&raw); err != nil {
			t.Fatal("read PDLC predecessor owner metadata")
		}
		return raw
	}
	before := metadata()
	for attempt := 0; attempt < 2; attempt++ {
		if err := goose.UpToContext(ctx, database, "migrations", 20261029000300); err != nil {
			t.Fatal("apply PDLC helper migration", err)
		}
		if !bytes.Equal(before, metadata()) {
			t.Fatal("PDLC migration changed existing function identity or authority")
		}
	}
	var count, applied int
	var closed bool
	if err := observer.QueryRow(ctx, `SELECT count(*),bool_and(proowner='control_plane_owner'::regrole AND NOT prosecdef AND proacl IS NOT NULL
AND has_function_privilege('control_plane_runtime_g1',oid,'EXECUTE')
AND NOT has_function_privilege('kodex_backup_reader',oid,'EXECUTE')
AND NOT EXISTS(SELECT 1 FROM aclexplode(proacl) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))
FROM pg_proc WHERE pronamespace='control_plane'::regnamespace AND proname IN
('workflow_pdlc_file_key','workflow_pdlc_file_desired_digest','workflow_pdlc_file_epoch_digest','workflow_pdlc_result_digest','workflow_evidence_delivery_operation','workflow_pdlc_read_order','workflow_pdlc_author_slot','workflow_pdlc_intent_valid',
'workflow_receipt_effect_proof','workflow_receipt_lineage_proof','workflow_native_receipt_proof','workflow_pdlc_receipt_fact','workflow_pdlc_result_valid','workflow_pdlc_outcome_valid')`).Scan(&count, &closed); err != nil || count != 14 || !closed {
		t.Fatal("PDLC helper ACL or function cardinality changed", err)
	}
	if err := observer.QueryRow(ctx, `SELECT count(*) FROM public.goose_db_version WHERE version_id=20261029000300 AND is_applied`).Scan(&applied); err != nil || applied != 1 {
		t.Fatal("PDLC no-op upgrade appended another migration")
	}
	tx, err := observer.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("begin readonly PDLC function probe")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE control_plane_runtime_g1"); err != nil {
		t.Fatal("use actual runtime identity for PDLC functions")
	}
	for _, profile := range []string{"", "UNKNOWN", "GITHUB_REVIEW_QA_V1", "GITHUB_PDLC_V1"} {
		for _, operation := range []string{"github.pull_request.review.create", "github.issue.create", "github.repository.content.create", "github.repository.content.update", "github.pull_request.comment.create", "github.pull_request.ready", "github.pull_request.merge", "github.pull_request.read", "ssh.compose.deploy"} {
			var actual bool
			old := operation == "github.pull_request.review.create" || operation == "github.issue.create"
			author := operation == "github.repository.content.create" || operation == "github.repository.content.update" || operation == "github.pull_request.comment.create" || operation == "github.pull_request.ready"
			want := profile == "GITHUB_REVIEW_QA_V1" && old || profile == "GITHUB_PDLC_V1" && (old || author)
			if err := tx.QueryRow(ctx, `SELECT control_plane.workflow_evidence_delivery_operation($1,$2)`, profile, operation).Scan(&actual); err != nil || actual != want {
				t.Fatal("native SQL delivery registry changed a profile boundary", profile, operation, err)
			}
		}
	}
	input := map[string]any{"path": "docs/требования<&>.md", "branch": "feature/journal", "message": "Уточнить документ", "content_base64": "0KLQvtGH0L3Ri9C1INGC0YDQtdCx0L7QstCw0L3QuNGPCg==", "sha": strings.Repeat("a", 40)}
	const key = "file_c0fbd812d8c3515c1da2ad37464f07cecfc25039a6e6a1918246b16718fa3b42"
	const desired = "19b0f4c753e3d1609bd0d7323fd408eb295d3faee77ec57d52fb11deca6dbea9"
	var actualKey, actualDesired, firstEpoch, nextEpoch string
	if err := tx.QueryRow(ctx, `SELECT control_plane.workflow_pdlc_file_key($1,$2),
control_plane.workflow_pdlc_file_desired_digest($3,$4,$5::jsonb),
control_plane.workflow_pdlc_file_epoch_digest($6,''),control_plane.workflow_pdlc_file_epoch_digest($6,'inv_previous0')`,
		"document", input["path"], key, strings.Repeat("b", 64), workflowPDLCTestJSON(t, input), desired).Scan(&actualKey, &actualDesired, &firstEpoch, &nextEpoch); err != nil || actualKey != key || actualDesired != desired ||
		firstEpoch != "fc21907c8bd4c92719235120193d9bbb75c071c4384fb7e0894d8bc5e89486e4" || nextEpoch != "9a6100ed16082a659c0f647f99b8ed3c592d22a33e1fb518f618d36b7e0463f7" {
		t.Fatal("native SQL file hashes differ from the fixed UTF8 byte-framing contract", err)
	}
	for _, vector := range []struct {
		parts []string
		want  string
	}{
		{[]string{"kodex.workflow-repository-file.v1", "document", "acme/repo", "feature/journal", "docs/требования<&>.md", "3", "erc_write0000", strings.Repeat("a", 64), "erc_head00000", strings.Repeat("b", 64), "erc_content00", strings.Repeat("c", 64)}, "55b72a8e26754afc9bbaadc7d01d3c5f5e91271c24f504392938c6498234c7d1"},
		{[]string{"kodex.workflow-pr-comment.v1", "response", "acme/repo", "3", "erc_comment00", strings.Repeat("d", 64), strings.Repeat("e", 64)}, "f97ba4bbc18ff97cd93adff14dd5cc8d6690d7b625cc962b86ffbf6bebb8362d"},
		{nil, ""}, {[]string{"unknown", "document"}, ""}, {[]string{"kodex.workflow-repository-file.v1", "document"}, ""},
	} {
		var actual sql.NullString
		if err := tx.QueryRow(ctx, `SELECT control_plane.workflow_pdlc_result_digest($1::text[])`, vector.parts).Scan(&actual); err != nil || actual.Valid != (vector.want != "") || actual.String != vector.want {
			t.Fatal("native SQL composite digest differs from the fixed Go/SQL contract", err)
		}
	}
	for _, mode := range []string{"empty", "utf8", "max-wrapped", "nul", "invalid-utf8", "oversize", "padding", "space", "tab"} {
		t.Run("text-"+mode, func(t *testing.T) {
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
			request := map[string]any{"path": "docs/requirements.md", "branch": "feature/journal", "message": "Уточнить требования", "content_base64": encoded}
			var digest sql.NullString
			valid := mode == "empty" || mode == "utf8" || mode == "max-wrapped"
			if err := tx.QueryRow(ctx, `SELECT control_plane.workflow_pdlc_file_desired_digest($1,$2,$3::jsonb)`, key, strings.Repeat("b", 64), workflowPDLCTestJSON(t, request)).Scan(&digest); err != nil || digest.Valid != valid {
				t.Fatal("native SQL text WRITE differs from the readable byte contract", mode, err)
			}
		})
	}
	requirement := map[string]any{"Key": "document", "Kind": "REPOSITORY_FILE", "Repository": "acme/repo", "Branch": "feature/journal", "TargetRef": "3"}
	for _, mode := range []string{"file", "foreign-repository", "foreign-branch", "traversal", "extra-input", "bad-base64", "null-content", "oversize", "comment", "comment-foreign-pr", "comment-blank", "ready", "ready-bad-head", "unpublished-merge"} {
		t.Run(mode, func(t *testing.T) {
			r, request := map[string]any{}, map[string]any{}
			for k, v := range requirement {
				r[k] = v
			}
			for k, v := range input {
				request[k] = v
			}
			scope := map[string]string{"owner": "acme", "repository": "repo"}
			op, want := "github.repository.content.update", key
			switch mode {
			case "foreign-repository":
				scope["repository"] = "foreign"
			case "foreign-branch":
				request["branch"] = "main"
			case "traversal":
				request["path"] = "docs/../.env"
			case "extra-input":
				request["actor"] = "owner"
			case "bad-base64":
				request["content_base64"] = "invalid:base64"
			case "null-content":
				request["content_base64"] = nil
			case "oversize":
				request["content_base64"] = strings.Repeat("YWFh", 87383)
			case "comment", "comment-foreign-pr", "comment-blank":
				r["Kind"], op, want = "PR_COMMENT", "github.pull_request.comment.create", "document"
				request = map[string]any{"pull_request_number": 3, "body": "Документы обновлены"}
				if mode == "comment-foreign-pr" {
					request["pull_request_number"] = 4
				}
				if mode == "comment-blank" {
					request["body"] = " \n\t "
				}
			case "ready", "ready-bad-head":
				r["Kind"], op, want = "PULL_REQUEST", "github.pull_request.ready", "ready_"+""
				request = map[string]any{"pull_request_number": 3, "expected_head_sha": strings.Repeat("a", 40)}
				if mode == "ready-bad-head" {
					request["expected_head_sha"] = "main"
				}
			case "unpublished-merge":
				op = "github.pull_request.merge"
			}
			var got sql.NullString
			err := tx.QueryRow(ctx, `SELECT control_plane.workflow_pdlc_author_slot($1::jsonb,$2,$3::jsonb,$4::jsonb)`, workflowPDLCTestJSON(t, r), op, workflowPDLCTestJSON(t, scope), workflowPDLCTestJSON(t, request)).Scan(&got)
			positive := mode == "file" || mode == "comment" || mode == "ready"
			if err != nil || got.Valid != positive {
				t.Fatal("native PDLC selector did not close target/input drift", mode, err)
			}
			if positive && mode != "ready" && got.String != want {
				t.Fatal("native PDLC selector changed logical result key")
			}
			if mode == "ready" && (!strings.HasPrefix(got.String, "ready_") || len(got.String) != 70) {
				t.Fatal("native Ready key is not bounded")
			}
		})
	}
	var unassigned bool
	if err := tx.QueryRow(ctx, `SELECT control_plane.workflow_pdlc_intent_valid('00000000-0000-4000-8000-000000000001','00000000-0000-4000-8000-000000000002',
'00000000-0000-4000-8000-000000000003','00000000-0000-4000-8000-000000000004','{}'::jsonb,'{}'::jsonb)`).Scan(&unassigned); err != nil || unassigned {
		t.Fatal("unassigned PDLC caller obtained an accepted intent", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("close readonly PDLC function probe")
	}
	if err := goose.DownContext(ctx, database, "migrations"); err == nil {
		t.Fatal("PDLC migration unexpectedly rolled backward")
	}
	var version int64
	if err := observer.QueryRow(ctx, `SELECT max(version_id) FROM public.goose_db_version WHERE is_applied`).Scan(&version); err != nil || version != 20261029000300 {
		t.Fatal("rejected PDLC down altered durable migration state")
	}
	t.Log("PDLC native helper/ACL/hash/selector proof passed; full guards, backup CLI and wire4 are not qualified by this suite")
}

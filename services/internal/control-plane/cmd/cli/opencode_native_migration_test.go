package main

import (
	_ "embed"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/pressly/goose/v3"
)

// nativeCutoverSeed creates inert synthetic configuration and a closed session only in the gated disposable DB.
// nativeCutoverSeed создаёт неактивную synthetic configuration и closed session только в disposable DB.
//
//go:embed testdata/opencode_native_cutover_seed.sql
var nativeCutoverSeed string

// TestOpenCodeNativeSessionCutover verifies the real forward-only migration and rejects implicit data erasure.
// It then deletes only its own empty synthetic session, preserving the provider/project/owner configuration.
// TestOpenCodeNativeSessionCutover проверяет настоящую forward-only миграцию и запрещает неявное стирание данных.
// Затем удаляет лишь собственную пустую synthetic session, сохраняя provider/project/owner configuration.
func TestOpenCodeNativeSessionCutover(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261111000100); err != nil {
		t.Fatal("prepare native session predecessor", err)
	}
	if _, err := observer.Exec(ctx, nativeCutoverSeed); err != nil {
		t.Fatal("prepare synthetic session fixture", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261111000200); err == nil || !strings.Contains(err.Error(), "explicitly authorized session-data purge") {
		t.Fatal("nonempty session cutover did not fail closed", err)
	}
	var sessions int
	if err := observer.QueryRow(ctx, `SELECT count(*) FROM control_plane.sessions WHERE ref='ses_native_cutover'`).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatal("blocked migration changed historical sessions", err)
	}
	if _, err := observer.Exec(ctx, `DELETE FROM control_plane.sessions WHERE ref='ses_native_cutover'`); err != nil {
		t.Fatal("remove own empty disposable session", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261111000200); err != nil {
		t.Fatal("apply native session cutover", err)
	}
	var preserved bool
	if err := observer.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM control_plane.projects WHERE ref='prj_native_cutover')
 AND EXISTS(SELECT 1 FROM control_plane.subjects WHERE ref='usr_native_cutover')
 AND EXISTS(SELECT 1 FROM control_plane.provider_accounts WHERE ref='pacc_native_cutover')
 AND NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='control_plane'
   AND table_name IN('session_storage','session_archives') AND column_name='codex_session_id')`).Scan(&preserved); err != nil || !preserved {
		t.Fatal("cutover lost configuration or retained old runtime column", err)
	}
	path, _ := runtimecontract.NativeSessionExportPath("ses_native", 1)
	value := runtimecontract.NativeSessionCheckpoint{SessionID: "ses_native", Version: runtimecontract.OpenCodeVersion, Generation: 1, RelativePath: path, SHA256: strings.Repeat("a", 64), SizeBytes: 800}
	raw, _ := json.Marshal(value)
	for _, test := range []struct {
		raw  []byte
		want bool
	}{
		{raw, true}, {[]byte(`{}`), false}, {[]byte(`[]`), false},
		{[]byte(strings.Replace(string(raw), "{", `{"secret":"synthetic",`, 1)), false},
		{[]byte(strings.Replace(string(raw), `"size_bytes":800`, `"size_bytes":67108865`, 1)), false},
		{[]byte(strings.Replace(string(raw), `"generation":1`, `"generation":2`, 1)), false},
	} {
		var valid bool
		if err := observer.QueryRow(ctx, `SELECT control_plane.native_checkpoint_shape($1::jsonb)`, string(test.raw)).Scan(&valid); err != nil || valid != test.want {
			t.Fatal("native metadata constraint disagrees with current contract", err)
		}
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261111000200); err != nil {
		t.Fatal("replay native migration", err)
	}
}

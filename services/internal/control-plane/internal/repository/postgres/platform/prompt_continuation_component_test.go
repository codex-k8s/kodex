package platform

import (
	"context"
	"encoding/json"
	"testing"
)

func assertContinuationNoticeReadback(t *testing.T, ctx context.Context, repository *Repository, runRef string) {
	t.Helper()
	var count int
	var content, materializationDigest, snapshotDigest string
	var rawMessages []byte
	err := repository.pool.QueryRow(ctx, `
		SELECT count(*) OVER (), notice.content, notice.materialization_digest,
		       revision.safe_snapshot->>'continuationNoticeDigest',
		       revision.safe_snapshot->'sessionContext'
		FROM control_plane.session_continuation_notices notice
		JOIN control_plane.runtime_revisions revision ON revision.id=notice.current_runtime_revision_id
		JOIN control_plane.runs run ON run.id=revision.run_id
		WHERE run.ref=$1 AND notice.node_id=revision.node_id
		  AND notice.turn_id=revision.turn_id AND notice.session_id=revision.session_id
		  AND notice.organization_id=revision.organization_id AND notice.attempt=revision.attempt
	`, runRef).Scan(&count, &content, &materializationDigest, &snapshotDigest, &rawMessages)
	if err != nil || count != 1 || materializationDigest == "" || materializationDigest != snapshotDigest {
		t.Fatalf("continuation notice lost immutable runtime binding: count=%d err=%v", count, err)
	}
	var messages []map[string]string
	if json.Unmarshal(rawMessages, &messages) != nil || len(messages) == 0 {
		t.Fatal("continuation notice is absent from runtime messages")
	}
	last := messages[len(messages)-1]
	if last["role"] != "USER" || last["content"] != content {
		t.Fatal("continuation notice differs from exact delivered USER message")
	}
}

package main

import (
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
)

func legacyPreimage(t *testing.T) string {
	t.Helper()
	raw, err := internalrpcauth.CanonicalJSON(map[string]string{"manifest_bundle": "fixture-public-bundle", "policy": "fixture-policy", "registry_digest_sha256": strings.Repeat("d", 64)})
	if err != nil {
		t.Fatal("encode legacy preimage")
	}
	return string(raw)
}

func seedLegacyRegistryProvenance(t *testing.T, port uint64) {
	ctx := t.Context()
	admin := workloadBoundaryConnection(t, ctx, port, "postgres", "")
	boundaryExec(t, ctx, admin, `INSERT INTO internal_rpc_authority.authority_rotation_intents
 (intent_id,source_revision,source_digest_sha256,status,created_at,updated_at)
 VALUES('14650000-0000-4000-8000-000000000001',100,repeat('c',64),'PROMOTED',clock_timestamp(),clock_timestamp())`)
	boundaryExec(t, ctx, admin, `INSERT INTO internal_rpc_authority.authority_snapshot_history
 (source_revision,source_digest_sha256,key_set_revision,policy_revision,signer_generation,predecessor_revision,predecessor_digest_sha256,canonical_payload,published_at,snapshot_compact_jws,publication_intent_id,publication_input_digest_sha256,expected_readback_count)
 VALUES(100,repeat('c',64),1,1,1,99,repeat('b',64),'{}',clock_timestamp(),'fixture.public.signature','14650000-0000-4000-8000-000000000001',encode(sha256(convert_to($1,'UTF8')),'hex'),1)`, legacyPreimage(t))
}

func testLegacyRegistryProvenance(t *testing.T, port uint64) {
	ctx := t.Context()
	admin := workloadBoundaryConnection(t, ctx, port, "postgres", "")
	migrator := workloadBoundaryConnection(t, ctx, port, "internal_rpc_authority_migrator", "internal_rpc_authority_readback_owner")
	publisher := workloadBoundaryConnection(t, ctx, port, "ira_publisher_g4", "internal_rpc_authority_publisher")
	var badCopy bool
	if err := admin.QueryRow(ctx, `SELECT registry_source_digest_sha256=source_digest_sha256 AND protocol_version=1 FROM internal_rpc_authority.authority_rotation_intents WHERE source_revision=100`).Scan(&badCopy); err != nil || !badCopy {
		t.Fatal("legacy migration fixture was not reproduced")
	}
	count := func() int {
		var n int
		if err := publisher.QueryRow(ctx, `SELECT count(*) FROM internal_rpc_authority.publisher_load_snapshot_predecessor(100,repeat('c',64),repeat('d',64))`).Scan(&n); err != nil {
			t.Fatal("predecessor lookup failed")
		}
		return n
	}
	if count() != 0 {
		t.Fatal("missing legacy proof accepted")
	}
	boundaryDenied(t, ctx, publisher, `SELECT internal_rpc_authority.repair_legacy_registry_provenance(100,repeat('c',64),'{}')`)
	boundaryDenied(t, ctx, publisher, `SELECT * FROM internal_rpc_authority.authority_legacy_registry_provenance`)
	repair := func(c *pgx.Conn, revision int64, digest, proof string) bool {
		var accepted bool
		err := c.QueryRow(ctx, `SELECT internal_rpc_authority.repair_legacy_registry_provenance($1,$2,$3)`, revision, digest, proof).Scan(&accepted)
		if err != nil {
			t.Fatal("repair call failed")
		}
		return accepted
	}
	for _, proof := range []string{"{", "[]", "{}", legacyPreimage(t) + " ", strings.Replace(legacyPreimage(t), "fixture-policy", "wrong-policy", 1), strings.Replace(legacyPreimage(t), strings.Repeat("d", 64), strings.Repeat("e", 64), 1)} {
		if repair(migrator, 100, strings.Repeat("c", 64), proof) {
			t.Fatal("mismatching preimage accepted")
		}
	}
	if repair(migrator, 101, strings.Repeat("c", 64), legacyPreimage(t)) || repair(migrator, 100, strings.Repeat("e", 64), legacyPreimage(t)) {
		t.Fatal("foreign history accepted")
	}
	for range 2 {
		if !repair(migrator, 100, strings.Repeat("c", 64), legacyPreimage(t)) {
			t.Fatal("exact legacy proof rejected")
		}
	}
	restarted := workloadBoundaryConnection(t, ctx, port, "internal_rpc_authority_migrator", "internal_rpc_authority_readback_owner")
	if !repair(restarted, 100, strings.Repeat("c", 64), legacyPreimage(t)) || count() != 1 {
		t.Fatal("durable repair did not survive reconnect")
	}
	var original bool
	if err := admin.QueryRow(ctx, `SELECT i.registry_source_digest_sha256=i.source_digest_sha256 AND h.source_digest_sha256=repeat('c',64) AND (SELECT count(*) FROM internal_rpc_authority.authority_legacy_registry_provenance WHERE source_revision=100)=1 FROM internal_rpc_authority.authority_rotation_intents i JOIN internal_rpc_authority.authority_snapshot_history h USING(source_revision) WHERE h.source_revision=100`).Scan(&original); err != nil || !original {
		t.Fatal("repair changed immutable history or duplicated receipt")
	}
	// Отдельный synthetic range не должен влиять на соседние component-сценарии.
	boundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_legacy_registry_provenance WHERE source_revision=100`)
	boundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_snapshot_history WHERE source_revision=100`)
	boundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_rotation_intents WHERE source_revision=100`)
}

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func testAuthorityNormalRotationOperation(t *testing.T, port uint64) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	admin := workloadBoundaryConnection(t, ctx, port, "postgres", "")
	publisher := workloadBoundaryConnection(t, ctx, port, "ira_publisher_g4", "internal_rpc_authority_publisher")
	publisherTwo := workloadBoundaryConnection(t, ctx, port, "ira_publisher_g4", "internal_rpc_authority_publisher")
	boundaryDenied(t, ctx, publisher, `SELECT * FROM internal_rpc_authority.authority_rotation_operations`)
	boundaryDenied(t, ctx, publisher, `SELECT * FROM internal_rpc_authority.authority_rotation_operation_phase_intents`)
	boundaryDenied(t, ctx, publisher, `SELECT * FROM internal_rpc_authority.authority_rotation_operation_publications`)

	baseRevision := int64(1093002)
	registryRevision := baseRevision + 1
	operationID := "21900000-0000-4000-8000-000000000001"
	registryDigest := strings.Repeat("6", 64)
	baseDigest := strings.Repeat("b", 64)
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_intents
		SET overlap_until=clock_timestamp()-interval '1 second'
		WHERE intent_id='10930000-0000-4000-8000-000000000011'`)

	loadFrom := func(connection *pgx.Conn) (string, *time.Time, *time.Time) {
		t.Helper()
		var status string
		var switchAt, previousAt *time.Time
		err := connection.QueryRow(ctx, `SELECT status, switch_not_before, previous_not_after
			FROM internal_rpc_authority.publisher_load_or_prepare_rotation_operation(
				$1,$2,$3,$4,$5,2)`, operationID, registryRevision,
			registryDigest, baseRevision, baseDigest).Scan(&status, &switchAt, &previousAt)
		if err != nil {
			t.Fatal("load normal rotation operation")
		}
		return status, switchAt, previousAt
	}
	load := func() (string, *time.Time, *time.Time) { return loadFrom(publisher) }
	type concurrentResult struct {
		status string
		err    error
	}
	results := make(chan concurrentResult, 2)
	for _, connection := range []*pgx.Conn{publisher, publisherTwo} {
		go func() {
			var status string
			err := connection.QueryRow(ctx, `SELECT status
				FROM internal_rpc_authority.publisher_load_or_prepare_rotation_operation(
					$1,$2,$3,$4,$5,2)`, operationID, registryRevision,
				registryDigest, baseRevision, baseDigest).Scan(&status)
			results <- concurrentResult{status: status, err: err}
		}()
	}
	for range 2 {
		result := <-results
		if result.err != nil || result.status != "DISTRIBUTING" {
			t.Fatal("concurrent exact operation did not converge")
		}
	}
	var rejectedStatus string
	err := publisher.QueryRow(ctx, `SELECT status
		FROM internal_rpc_authority.publisher_load_or_prepare_rotation_operation(
			$1,$2,$3,$4,$5,2)`, "21900000-0000-4000-8000-000000000099",
		baseRevision+1, strings.Repeat("9", 64), baseRevision, baseDigest).Scan(&rejectedStatus)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("parallel different rotation operation was accepted")
	}
	if status, _, _ := load(); status != "DISTRIBUTING" {
		t.Fatal("normal rotation operation was not prepared")
	}
	lastSnapshotDigest := baseDigest
	phase := func(name string, offset int64, snapshotDigest, inputDigest string, suffix int) string {
		t.Helper()
		intent := fmt.Sprintf("21900000-0000-4000-8000-%012d", suffix)
		revision := baseRevision + offset
		predecessorRevision := revision - 1
		predecessorDigest := lastSnapshotDigest
		var accepted bool
		for range 2 {
			if err := publisher.QueryRow(ctx, `SELECT internal_rpc_authority.publisher_prepare_rotation_phase(
				$1,$2,$3,$4,$5,$6,$7,2,$8)`, operationID, name, intent,
				revision, registryDigest, predecessorRevision, predecessorDigest, inputDigest).Scan(&accepted); err != nil || !accepted {
				t.Fatalf("prepare %s phase", name)
			}
		}
		conflictingIntent := fmt.Sprintf("21900000-0000-4000-8000-%012d", suffix+100)
		if err := publisher.QueryRow(ctx, `SELECT internal_rpc_authority.publisher_prepare_rotation_phase(
			$1,$2,$3,$4,$5,$6,$7,2,$8)`, operationID, name, conflictingIntent,
			revision, registryDigest, predecessorRevision, predecessorDigest, inputDigest).Scan(&accepted); err != nil || accepted {
			t.Fatalf("conflicting %s phase intent was accepted", name)
		}
		var conflictingRows int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM internal_rpc_authority.authority_rotation_intents
			WHERE intent_id=$1`, conflictingIntent).Scan(&conflictingRows); err != nil || conflictingRows != 0 {
			t.Fatalf("conflicting %s phase intent left durable state", name)
		}
		if err := publisher.QueryRow(ctx, `SELECT internal_rpc_authority.publisher_begin_rotation_delivery($1,$2,$3)`,
			intent, revision, registryDigest).Scan(&accepted); err != nil || !accepted {
			t.Fatalf("begin %s delivery", name)
		}
		if err := publisher.QueryRow(ctx, `SELECT internal_rpc_authority.publisher_append_snapshot_history(
			$1,$2,$1,1,1,$3,$4,repeat('j',64),$5,$6,2,clock_timestamp())`,
			revision, snapshotDigest, predecessorRevision, predecessorDigest, intent, inputDigest).Scan(&accepted); err != nil || !accepted {
			t.Fatalf("append %s publication", name)
		}
		if err := publisher.QueryRow(ctx, `SELECT internal_rpc_authority.publisher_mark_rotation_delivered($1,$2,$3)`,
			intent, revision, snapshotDigest).Scan(&accepted); err != nil || !accepted {
			t.Fatalf("mark %s delivered", name)
		}
		for index, role := range []string{"AUTHORIZATION_ISSUER", "AUTHORIZATION_VERIFIER"} {
			boundaryExec(t, ctx, admin, `INSERT INTO internal_rpc_authority.authority_snapshot_readbacks
				(readback_id,workload_id,role,workload_generation,source_revision,digest_sha256,verified_at)
				VALUES ($1,$2,$3,1,$4,$5,clock_timestamp())`,
				fmt.Sprintf("21910000-0000-4000-8000-%012d", suffix*10+index),
				[]string{"rotation-required-a", "rotation-required-b"}[index], role, revision, snapshotDigest)
		}
		if err := publisher.QueryRow(ctx, `SELECT internal_rpc_authority.publisher_promote_snapshot(
			$1,$2,$3,2,ARRAY['rotation-required-a','rotation-required-b'],
			ARRAY['AUTHORIZATION_ISSUER','AUTHORIZATION_VERIFIER'],ARRAY[1,1]::bigint[])`,
			intent, revision, snapshotDigest).Scan(&accepted); err != nil || !accepted {
			t.Fatalf("promote %s publication", name)
		}
		var status string
		if err := publisher.QueryRow(ctx, `SELECT status FROM internal_rpc_authority.publisher_advance_rotation_operation(
			$1,$2,$3,$4,$5,2,$6,$7,$8,$9,$10)`, operationID, registryRevision,
			registryDigest, baseRevision, baseDigest, name, intent, revision, snapshotDigest, inputDigest).Scan(&status); err != nil {
			t.Fatalf("advance %s phase", name)
		}
		lastSnapshotDigest = snapshotDigest
		return status
	}

	distributeDigest := strings.Repeat("5", 64)
	if status := phase("DISTRIBUTE", 1, distributeDigest, strings.Repeat("a", 64), 1); status != "WAITING_SWITCH" {
		t.Fatal("DISTRIBUTE did not reach WAITING_SWITCH")
	}
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_operations
		SET switch_not_before=clock_timestamp()-interval '1 second' WHERE operation_id=$1`, operationID)
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_intents
		SET overlap_until=clock_timestamp()-interval '1 second' WHERE intent_id::text LIKE '21900000-0000-4000-8000-%'`)
	status, _, previousDeadline := load()
	if status != "SWITCHING" || previousDeadline == nil {
		t.Fatal("database did not assign fixed PREVIOUS deadline")
	}
	fixedDeadline := *previousDeadline
	if status := phase("SWITCH", 2, strings.Repeat("7", 64), strings.Repeat("c", 64), 2); status != "WAITING_RETIRE" {
		t.Fatal("SWITCH did not reach WAITING_RETIRE")
	}
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_operations
		SET previous_not_after=clock_timestamp()-interval '1 second' WHERE operation_id=$1`, operationID)
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_intents
		SET overlap_until=clock_timestamp()-interval '1 second' WHERE intent_id::text LIKE '21900000-0000-4000-8000-%'`)
	status, _, previousDeadline = load()
	if status != "RETIRING" || previousDeadline == nil || !previousDeadline.Before(fixedDeadline) {
		t.Fatal("RETIRE did not use persisted deadline")
	}
	if status := phase("RETIRE", 3, strings.Repeat("8", 64), strings.Repeat("d", 64), 3); status != "RETIRED" {
		t.Fatal("RETIRE did not complete operation")
	}
	firstOperationID := operationID
	firstCompletedDigest := lastSnapshotDigest
	var firstSwitchDeadline, firstPreviousDeadline, firstCompletedAt time.Time
	if err := admin.QueryRow(ctx, `SELECT switch_not_before,previous_not_after,completed_at
		FROM internal_rpc_authority.authority_rotation_operations WHERE operation_id=$1`, firstOperationID).
		Scan(&firstSwitchDeadline, &firstPreviousDeadline, &firstCompletedAt); err != nil {
		t.Fatal("read first completed rotation deadlines")
	}
	rejoinedPublisher := workloadBoundaryConnection(t, ctx, port, "ira_publisher_g4", "internal_rpc_authority_publisher")
	if status, switchDeadline, previousDeadline := loadFrom(rejoinedPublisher); status != "RETIRED" ||
		switchDeadline == nil || previousDeadline == nil || !switchDeadline.Equal(firstSwitchDeadline) ||
		!previousDeadline.Equal(firstPreviousDeadline) {
		t.Fatal("first retired rotation did not rejoin with immutable deadlines")
	}
	publisher = rejoinedPublisher

	// Registry revision advances once per operation, while each completed normal
	// rotation advances the independent snapshot history by three revisions.
	baseRevision += 3
	baseDigest = firstCompletedDigest
	registryRevision++
	operationID = "21900000-0000-4000-8000-000000000002"
	registryDigest = strings.Repeat("9", 64)
	if status, _, _ := load(); status != "DISTRIBUTING" {
		t.Fatal("second normal rotation did not start from retired predecessor")
	}
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_intents
		SET overlap_until=clock_timestamp()-interval '1 second' WHERE intent_id::text LIKE '21900000-0000-4000-8000-%'`)
	if status := phase("DISTRIBUTE", 1, strings.Repeat("1", 64), strings.Repeat("e", 64), 11); status != "WAITING_SWITCH" {
		t.Fatal("second DISTRIBUTE did not reach WAITING_SWITCH")
	}
	secondStatus, secondSwitchDeadline, _ := load()
	if secondStatus != "WAITING_SWITCH" || secondSwitchDeadline == nil {
		t.Fatal("second rotation switch deadline was not persisted")
	}
	if _, repeatedSwitchDeadline, _ := load(); repeatedSwitchDeadline == nil || !repeatedSwitchDeadline.Equal(*secondSwitchDeadline) {
		t.Fatal("second rotation restart changed switch deadline")
	}
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_operations
		SET switch_not_before=clock_timestamp()-interval '1 second' WHERE operation_id=$1`, operationID)
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_intents
		SET overlap_until=clock_timestamp()-interval '1 second' WHERE intent_id::text LIKE '21900000-0000-4000-8000-%'`)
	secondStatus, _, secondPreviousDeadline := load()
	if secondStatus != "SWITCHING" || secondPreviousDeadline == nil {
		t.Fatal("second rotation PREVIOUS deadline was not assigned")
	}
	if _, _, repeatedPreviousDeadline := load(); repeatedPreviousDeadline == nil || !repeatedPreviousDeadline.Equal(*secondPreviousDeadline) {
		t.Fatal("second rotation restart changed PREVIOUS deadline")
	}
	if status := phase("SWITCH", 2, strings.Repeat("2", 64), strings.Repeat("f", 64), 12); status != "WAITING_RETIRE" {
		t.Fatal("second SWITCH did not reach WAITING_RETIRE")
	}
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_operations
		SET previous_not_after=clock_timestamp()-interval '1 second' WHERE operation_id=$1`, operationID)
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_intents
		SET overlap_until=clock_timestamp()-interval '1 second' WHERE intent_id::text LIKE '21900000-0000-4000-8000-%'`)
	secondStatus, secondExpiredSwitchDeadline, secondExpiredPreviousDeadline := load()
	if secondStatus != "RETIRING" || secondExpiredSwitchDeadline == nil || secondExpiredPreviousDeadline == nil {
		t.Fatal("second rotation did not enter RETIRING at persisted deadline")
	}
	secondRetireDigest := strings.Repeat("3", 64)
	if status := phase("RETIRE", 3, secondRetireDigest, strings.Repeat("4", 64), 13); status != "RETIRED" {
		t.Fatal("second RETIRE did not remove PREVIOUS and complete operation")
	}
	var registryProgress, snapshotProgress int64
	if err := admin.QueryRow(ctx, `SELECT max(registry_revision)-min(registry_revision)
		FROM internal_rpc_authority.authority_rotation_operations WHERE operation_id IN ($1,$2)`, firstOperationID, operationID).Scan(&registryProgress); err != nil || registryProgress != 1 {
		t.Fatal("registry revision did not advance once across two rotations")
	}
	if err := admin.QueryRow(ctx, `SELECT max(source_revision)-$1
		FROM internal_rpc_authority.authority_rotation_operation_publications WHERE operation_id IN ($2,$3)`, int64(1093002), firstOperationID, operationID).Scan(&snapshotProgress); err != nil || snapshotProgress != 6 {
		t.Fatal("snapshot revision did not advance by three per rotation")
	}
	var secondRetirePublications int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM internal_rpc_authority.authority_rotation_operation_publications
		WHERE operation_id=$1 AND phase='RETIRE' AND source_revision=$2 AND snapshot_digest_sha256=$3`,
		operationID, baseRevision+3, secondRetireDigest).Scan(&secondRetirePublications); err != nil || secondRetirePublications != 1 {
		t.Fatal("second PREVIOUS removal publication was not persisted exactly once")
	}
	var secondImmutableDeadlines bool
	if err := admin.QueryRow(ctx, `SELECT status='RETIRED' AND registry_revision=$2 AND base_revision=$3
		AND switch_not_before=$4 AND previous_not_after=$5 AND completed_at IS NOT NULL
		FROM internal_rpc_authority.authority_rotation_operations WHERE operation_id=$1`, operationID,
		registryRevision, baseRevision, *secondExpiredSwitchDeadline, *secondExpiredPreviousDeadline).
		Scan(&secondImmutableDeadlines); err != nil || !secondImmutableDeadlines {
		t.Fatal("second retired rotation changed persisted counters or deadlines")
	}
	var firstStillRetired bool
	if err := admin.QueryRow(ctx, `SELECT status='RETIRED' AND completed_at=$2 AND switch_not_before=$3 AND previous_not_after=$4
		FROM internal_rpc_authority.authority_rotation_operations WHERE operation_id=$1`, firstOperationID,
		firstCompletedAt, firstSwitchDeadline, firstPreviousDeadline).Scan(&firstStillRetired); err != nil || !firstStillRetired {
		t.Fatal("second rotation mutated first retired operation")
	}
	var publications int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM internal_rpc_authority.authority_rotation_operation_publications
		WHERE operation_id IN ($1,$2) AND registry_digest_sha256<>snapshot_digest_sha256`, firstOperationID, operationID).Scan(&publications); err != nil || publications != 6 {
		t.Fatal("six immutable cross-domain publications were not persisted")
	}
	migrator := workloadBoundaryConnection(t, ctx, port, "internal_rpc_authority_migrator", "internal_rpc_authority_readback_owner")
	var operationStatus string
	var statusDeadline *time.Time
	if err := migrator.QueryRow(ctx, `SELECT result->>'operationStatus', (result->>'previousNotAfter')::timestamptz
		FROM (SELECT internal_rpc_authority.authority_rotation_status() AS result) AS status`).Scan(&operationStatus, &statusDeadline); err != nil || operationStatus != "RETIRED" || statusDeadline == nil {
		t.Fatal("operator compound rotation readback mismatch")
	}

	boundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_snapshot_readbacks WHERE readback_id::text LIKE '2191%'`)
	boundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_rotation_operation_publications WHERE operation_id IN ($1,$2)`, firstOperationID, operationID)
	boundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_rotation_operation_phase_intents WHERE operation_id IN ($1,$2)`, firstOperationID, operationID)
	boundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_rotation_operations WHERE operation_id IN ($1,$2)`, firstOperationID, operationID)
	boundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_snapshot_history WHERE source_revision BETWEEN $1 AND $2`, int64(1093003), baseRevision+3)
	boundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_rotation_intents WHERE intent_id::text LIKE '2190%'`)
	boundaryExec(t, ctx, admin, `UPDATE internal_rpc_authority.authority_rotation_intents
		SET status='PROMOTED',retired_at=NULL,overlap_until=clock_timestamp()+interval '40 seconds'
		WHERE intent_id='10930000-0000-4000-8000-000000000011'`)
}

package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestRotationAbortArgumentsFailClosed accepts one canonical abort command and rejects six altered flag/identity/revision/digest/confirmation variants plus missing or extra arguments. It runs only parsers, not a database abort, role check or rotation-state transition.
// TestRotationAbortArgumentsFailClosed принимает одну каноническую команду отмены и отклоняет шесть изменений флага, идентичности, ревизии, дайджеста или подтверждения, а также неполные и лишние аргументы. Выполняются только разборщики без отмены в БД, проверки роли или перехода состояния ротации.
func TestRotationAbortArgumentsFailClosed(t *testing.T) {
	digest := strings.Repeat("a", 64)
	valid := []string{"rotation-abort", "--intent-id", "13900000-0000-4000-8000-000000000001", "--source-revision", "2", "--source-digest-sha256", digest, "--confirm", "ABORT-STAGING-AUTHORITY-ROTATION"}
	action, err := parseCommand(valid)
	if err != nil || action != commandRotationAbort {
		t.Fatal("rotation abort command rejected")
	}
	if _, err := parseRotationOptions(action, valid); err != nil {
		t.Fatal("rotation abort identity rejected")
	}
	for _, tc := range []struct {
		index int
		value string
	}{
		{1, "--force"}, {2, "00000000-0000-0000-0000-000000000000"},
		{4, "02"}, {4, "0"}, {6, strings.Repeat("A", 64)},
		{8, "ABORT-PRODUCTION-AUTHORITY-ROTATION"},
	} {
		args := append([]string(nil), valid...)
		args[tc.index] = tc.value
		if _, err := parseRotationOptions(action, args); err == nil {
			t.Fatalf("invalid rotation abort accepted: argument %d", tc.index)
		}
	}
	if _, err := parseCommand(valid[:8]); err == nil {
		t.Fatal("incomplete rotation abort accepted")
	}
	if _, err := parseCommand(append(valid, "--force")); err == nil {
		t.Fatal("additional rotation abort argument accepted")
	}
}

// TestRotationStatusKeepsRegistryAndSnapshotSequencesIndependent covers owner-valid first and later
// operations and rejects malformed bounds, missing provenance and invalid lifecycle fields. It does
// not replace the SQL owner's durable predecessor and complete-readback checks.
// TestRotationStatusKeepsRegistryAndSnapshotSequencesIndependent проверяет допустимые owner первые
// и последующие operations и отклоняет неверные границы, отсутствие provenance и lifecycle fields.
// Не заменяет проверку устойчивого predecessor и полного readback у SQL owner.
func TestRotationStatusKeepsRegistryAndSnapshotSequencesIndependent(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, revisions := range [][2]int64{{2, 1}, {3, 4}, {9, 14}} {
		registry, base, expected, actual := revisions[0], revisions[1], 16, 16
		status := rotationStatus{ObservedAt: now, OperationID: "13900000-0000-4000-8000-000000000002",
			Status: "RETIRED", OperationStatus: "RETIRED", Phase: "RETIRE", RegistryRevision: &registry, BaseRevision: &base,
			RegistrySourceDigestSHA256: strings.Repeat("a", 64), BaseDigestSHA256: strings.Repeat("b", 64),
			ExpectedReadbacks: &expected, ActualReadbacks: &actual, SwitchNotBefore: &now, PreviousNotAfter: &now, CompletedAt: &now,
		}
		raw, err := json.Marshal(status)
		if err != nil {
			t.Fatal("encode synthetic owner status")
		}
		decoded, err := decodeRotationStatus(raw)
		if err != nil || decoded.RegistryRevision == nil || *decoded.RegistryRevision != registry || decoded.BaseRevision == nil || *decoded.BaseRevision != base {
			t.Fatalf("owner-valid registry=%d snapshot-base=%d was rejected", registry, base)
		}
		for _, mutate := range []func(*rotationStatus){
			func(value *rotationStatus) { value.RegistryRevision = nil },
			func(value *rotationStatus) { value.BaseRevision = nil },
			func(value *rotationStatus) { zero := int64(0); value.RegistryRevision = &zero },
			func(value *rotationStatus) { huge := int64(9007199254740992); value.RegistryRevision = &huge },
			func(value *rotationStatus) { huge := int64(9007199254740989); value.BaseRevision = &huge },
			func(value *rotationStatus) { value.ExpectedReadbacks = nil },
			func(value *rotationStatus) { huge := 385; value.ExpectedReadbacks = &huge },
			func(value *rotationStatus) { value.RegistrySourceDigestSHA256 = "unknown" },
			func(value *rotationStatus) { value.OperationStatus = "PROMOTED" },
			func(value *rotationStatus) { value.CompletedAt = nil },
		} {
			candidate := status
			mutate(&candidate)
			raw, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal("encode rejected owner status fixture")
			}
			if _, err := decodeRotationStatus(raw); err == nil {
				t.Fatal("invalid owner result passed CLI validation")
			}
		}
	}
}

// TestRotationWatchCommandIsExact checks one exact operation-watch command and its parsed identity, then rejects a malformed force form. It does not run the timed watch loop or connect to PostgreSQL.
// TestRotationWatchCommandIsExact проверяет одну точную команду наблюдения операции и разобранную идентичность, затем отклоняет некорректную форму force. Цикл наблюдения по времени и подключение PostgreSQL не запускаются.
func TestRotationWatchCommandIsExact(t *testing.T) {
	arguments := []string{"rotation-watch", "--operation-id", "13900000-0000-4000-8000-000000000002"}
	action, err := parseCommand(arguments)
	if err != nil || action != commandRotationWatch {
		t.Fatal("rotation watch command rejected")
	}
	options, err := parseRotationOptions(action, arguments)
	if err != nil || options.operationID != arguments[2] {
		t.Fatal("rotation watch operation identity rejected")
	}
	if _, err := parseCommand([]string{"rotation-watch", "--force"}); err == nil {
		t.Fatal("rotation watch accepted additional arguments")
	}
}

// TestRotationWatchRejectsForeignRetiredOperationAcrossRestart rejects a foreign RETIRED operation and an own WAITING_RETIRE state, then evaluates the matching terminal predicate twice. The repeated calls model restart-independent selection; no process restart, persistent-state read or timer loop occurs in this unit.
// TestRotationWatchRejectsForeignRetiredOperationAcrossRestart отклоняет чужую RETIRED-операцию и собственное состояние WAITING_RETIRE, затем дважды вычисляет совпадающий terminal-предикат. Повторные вызовы моделируют независимый от перезапуска выбор; реального перезапуска процесса, чтения устойчивого состояния или цикла таймера в юните нет.
func TestRotationWatchRejectsForeignRetiredOperationAcrossRestart(t *testing.T) {
	expected := "13900000-0000-4000-8000-000000000002"
	foreign := rotationStatus{OperationID: "13900000-0000-4000-8000-000000000003", Status: "RETIRED"}
	if rotationWatchComplete(foreign, expected) {
		t.Fatal("foreign retired operation completed watch")
	}
	waiting := rotationStatus{OperationID: expected, Status: "WAITING_RETIRE"}
	if rotationWatchComplete(waiting, expected) {
		t.Fatal("non-terminal owner operation completed watch")
	}
	retired := rotationStatus{OperationID: expected, Status: "RETIRED"}
	for restart := 0; restart < 2; restart++ {
		if !rotationWatchComplete(retired, expected) {
			t.Fatal("owner operation did not complete watch after restart")
		}
	}
}

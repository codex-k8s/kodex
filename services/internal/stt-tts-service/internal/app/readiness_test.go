package app

import (
	"context"
	"os"
	"testing"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/transcriptiontest"
	transportgrpc "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// readinessVerifier supplies a mutable local verifier-ready flag for testing propagation into one shared readiness snapshot.
// RU: readinessVerifier задаёт изменяемый локальный флаг готовности verifier для проверки его передачи в единый readiness снимок.
type readinessVerifier struct {
	internalrpcauthorityv1.AuthorizationVerifierServiceClient
	ready bool
}

// CheckReadiness returns the configured synthetic flag without requiring an authority socket or verifier workload.
// RU: CheckReadiness возвращает настроенный синтетический флаг без authority сокета или verifier workload.
func (verifier *readinessVerifier) CheckReadiness(
	context.Context,
	*internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest,
	...grpc.CallOption,
) (*internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse, error) {
	return &internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse{Ready: verifier.ready}, nil
}

// readinessMetric records the most recently published local ready gauge without registering Prometheus collectors.
// RU: readinessMetric сохраняет последнюю опубликованную локальную метрику готовности без регистрации Prometheus collector.
type readinessMetric struct{ ready bool }

// SetReady updates only the fixture gauge for comparison with the authoritative readiness snapshot.
// RU: SetReady обновляет только метрику оснастки для сравнения с авторитетным readiness снимком.
func (metric *readinessMetric) SetReady(ready bool) { metric.ready = ready }

// TestGRPCReadinessUsesSameSnapshotForVerifierAndSpoolDegradation checks consistent metric, local snapshot and RPC results across verifier failure and removed spool.
// RU: TestGRPCReadinessUsesSameSnapshotForVerifierAndSpoolDegradation проверяет согласованность метрики, локального снимка и RPC при отказе verifier и удалённом spool.
func TestGRPCReadinessUsesSameSnapshotForVerifierAndSpoolDegradation(t *testing.T) {
	spool := t.TempDir()
	verifier := &readinessVerifier{ready: true}
	checks := []checker{verifierReadiness{verifier}, spoolReadiness{spool}}
	readiness := serviceruntime.NewReadiness()
	metric := &readinessMetric{}
	server, err := transportgrpc.New(transcriptiontest.Service{}, spool, readiness, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	assert := func(wantReady bool) {
		t.Helper()
		snapshotReady, _ := readiness.Ready()
		response, rpcErr := server.CheckReadiness(t.Context(), &sttv1.CheckReadinessRequest{})
		if snapshotReady != wantReady || response.GetReady() != snapshotReady || metric.ready != snapshotReady {
			t.Fatalf("несогласованный readiness snapshot: want=%v snapshot=%v rpc=%v metric=%v", wantReady, snapshotReady, response.GetReady(), metric.ready)
		}
		if wantReady && rpcErr != nil {
			t.Fatalf("готовый snapshot отклонён: %v", rpcErr)
		}
		if !wantReady && status.Code(rpcErr) != codes.Unavailable {
			t.Fatalf("деградированный snapshot вернул code=%s err=%v", status.Code(rpcErr), rpcErr)
		}
	}

	if _, err := updateLocalReadiness(t.Context(), readiness, metric, checks...); err != nil {
		t.Fatal(err)
	}
	assert(true)

	verifier.ready = false
	if _, err := updateLocalReadiness(t.Context(), readiness, metric, checks...); err == nil {
		t.Fatal("деградация verifier не отражена")
	}
	assert(false)

	verifier.ready = true
	if err := os.RemoveAll(spool); err != nil {
		t.Fatal(err)
	}
	if _, err := updateLocalReadiness(t.Context(), readiness, metric, checks...); err == nil {
		t.Fatal("деградация spool не отражена")
	}
	assert(false)
}

package app

import (
	"context"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"os"
	"testing"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	transcriptionservice "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	transportgrpc "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type readinessVerifier struct {
	internalrpcauthorityv1.AuthorizationVerifierServiceClient
	ready bool
}

func (verifier *readinessVerifier) CheckReadiness(
	context.Context,
	*internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessRequest,
	...grpc.CallOption,
) (*internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse, error) {
	return &internalrpcauthorityv1.AuthorizationVerifierServiceCheckReadinessResponse{Ready: verifier.ready}, nil
}

type readinessMetric struct{ ready bool }

func (metric *readinessMetric) SetReady(ready bool) { metric.ready = ready }

type readinessService struct{}

func (readinessService) Catalog() modelprofile.Catalog { return modelprofile.OpenAICatalog() }

func (readinessService) GetModelCatalog(context.Context, value.Principal) (modelprofile.Catalog, error) {
	return modelprofile.OpenAICatalog(), nil
}

func (readinessService) CheckAvailability(context.Context, value.Principal, string) (transcriptionservice.Availability, error) {
	return transcriptionservice.Availability{}, errors.New("unavailable")
}

func (readinessService) Transcribe(context.Context, transcriptionservice.Input) (value.TranscriptionResult, error) {
	return value.TranscriptionResult{}, nil
}
func (readinessService) CheckLocal(context.Context) error         { return nil }
func (readinessService) CheckProtectedPath(context.Context) error { return errors.New("pending") }

func TestGRPCReadinessUsesSameSnapshotForVerifierAndSpoolDegradation(t *testing.T) {
	spool := t.TempDir()
	verifier := &readinessVerifier{ready: true}
	checks := []checker{verifierReadiness{verifier}, spoolReadiness{spool}}
	readiness := serviceruntime.NewReadiness()
	metric := &readinessMetric{}
	server, err := transportgrpc.New(readinessService{}, spool, readiness, time.Second)
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

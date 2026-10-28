package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/controller"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// archiveAckFixture counts physical execution separately from a transport-uncertain terminal acknowledgement.
// archiveAckFixture отдельно считает физическое исполнение и terminal ACK с uncertain transport outcome.
type archiveAckFixture struct {
	cp.SessionArchiveWorkServiceClient
	physical, acknowledgements int
	failure                    codes.Code
	alwaysFail                 bool
	first                      *cp.CompleteSessionSnapshotRequest
	changed                    bool
}

// ExecuteRetained models one completed physical action; no owner RPC or retry takes place here.
// ExecuteRetained моделирует одно завершённое физическое действие без owner RPC либо retry.
func (fixture *archiveAckFixture) ExecuteRetained(context.Context, model.Task, func(context.Context) error) (model.Result, *controller.RetainedAttempt, error) {
	fixture.physical++
	return model.Result{Success: true, FormatVersion: 1, ObjectKey: "fixture-object", ObjectVersion: "fixture-version",
		ObjectETag: "fixture-etag", ObjectDigest: "sha256:" + strings.Repeat("d", 64), ObjectSizeBytes: 1024, SourceSizeBytes: 128}, nil, nil
}

// CompleteSessionSnapshot loses the first response and rejects any change to the already submitted receipt bytes.
// CompleteSessionSnapshot теряет первый ответ и отмечает любое изменение уже отправленной квитанции.
func (fixture *archiveAckFixture) CompleteSessionSnapshot(_ context.Context, request *cp.CompleteSessionSnapshotRequest, _ ...grpc.CallOption) (*cp.CompleteSessionSnapshotResponse, error) {
	fixture.acknowledgements++
	if fixture.first == nil {
		fixture.first = proto.Clone(request).(*cp.CompleteSessionSnapshotRequest)
	} else if !proto.Equal(fixture.first, request) {
		fixture.changed = true
	}
	if fixture.acknowledgements == 1 || fixture.alwaysFail {
		return nil, status.Error(fixture.failure, "fixture transport outcome")
	}
	return &cp.CompleteSessionSnapshotResponse{}, nil
}

// TestArchiveAcknowledgementNeverRepeatsPhysicalExecution checks lost ACK, exhausted uncertainty and a definitive owner denial.
// TestArchiveAcknowledgementNeverRepeatsPhysicalExecution проверяет lost ACK, exhausted uncertainty и окончательный owner denial.
func TestArchiveAcknowledgementNeverRepeatsPhysicalExecution(t *testing.T) {
	for _, test := range []struct {
		name    string
		code    codes.Code
		always  bool
		calls   int
		success bool
	}{
		{"lost-ack", codes.Unavailable, false, 2, true},
		{"unknown-after-retry", codes.DeadlineExceeded, true, 2, false},
		{"owner-denied", codes.PermissionDenied, true, 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			pvc, err := runtimecontract.SessionPVCName("ses_public_fixture")
			if err != nil {
				t.Fatal(err)
			}
			claim := &cp.SessionArchiveTask{TaskRef: "sat_public_fixture", SessionRef: "ses_public_fixture", OrganizationRef: "org_public_fixture",
				Kind: cp.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_SNAPSHOT, ProviderAccountRef: "pacc_fixture",
				RuntimeRevisionRef: "rrev_fixture", RuntimeRevisionVersion: 1, RuntimeRevisionDigest: strings.Repeat("a", 64),
				CodexSessionId: "00000000-0000-4000-8000-000000000003", ContentGeneration: 3, PvcName: pvc, InputDigest: strings.Repeat("b", 64),
				SourceRelativePath: ".kodex/state/codex-home/sessions/2026/10/04/rollout-2026-10-04T00-00-00-00000000-0000-4000-8000-000000000003.jsonl",
				SourceSha256:       strings.Repeat("c", 64), SourceSizeBytes: 128, TargetObjectKey: "fixture-object", Attempt: 1,
				Lease: &cp.WorkLease{Ref: "lea_fixture", Fence: "fnc_fixture", Generation: 1}}
			fixture := &archiveAckFixture{failure: test.code, alwaysFail: test.always}
			err = process(t.Context(), &controlplaneclient.Client{SessionArchive: fixture}, fixture, claim, newArchiveMetrics(),
				slog.New(slog.NewTextHandler(io.Discard, nil)), Config{WorkerTimeout: time.Minute, RPCDeadline: time.Second})
			if (err == nil) != test.success || fixture.physical != 1 || fixture.acknowledgements != test.calls || fixture.changed || fixture.first == nil || fixture.first.GetMutation().GetIdempotencyKey() == "" {
				t.Fatal("archive acknowledgement repeated the physical effect, changed its receipt or lost a closed failure", err)
			}
		})
	}
}

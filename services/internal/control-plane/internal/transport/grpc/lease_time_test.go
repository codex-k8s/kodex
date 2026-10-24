package grpc

import (
	"reflect"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestLeaseExpiryRetainsTypedPins checks absent/invalid/epoch/nanosecond-precision expiry through
// both actual lease casters while preserving each distinct work/source/proposal tuple and nil input.
// TestLeaseExpiryRetainsTypedPins проверяет отсутствие, неверный, epoch и наносекундный expiry
// через оба настоящих lease casters с сохранением разных work/source/proposal tuples и nil input.
func TestLeaseExpiryRetainsTypedPins(t *testing.T) {
	instant := time.Date(2026, 10, 24, 12, 34, 56, 789123456, time.UTC)
	for _, test := range []struct {
		name  string
		input *timestamppb.Timestamp
		want  time.Time
	}{
		{"absent", nil, time.Time{}},
		{"valid", timestamppb.New(instant), instant},
		{"epoch", &timestamppb.Timestamp{}, time.Unix(0, 0).UTC()},
		{"negative nanos", &timestamppb.Timestamp{Seconds: 1, Nanos: -1}, time.Time{}},
		{"overflow nanos", &timestamppb.Timestamp{Seconds: 1, Nanos: 1_000_000_000}, time.Time{}},
		{"before minimum", &timestamppb.Timestamp{Seconds: -62135596801}, time.Time{}},
		{"after maximum", &timestamppb.Timestamp{Seconds: 253402300800}, time.Time{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := sourceLease(&cp.ManagedConfigurationSourceLease{WorkRef: "work_fixture", SourceGeneration: 3, Attempt: 5, ClaimGeneration: 7, Claimant: "worker_fixture", Fence: "source_fence", ExpiresAt: test.input})
			wantSource := entity.ManagedConfigurationSourceLease{WorkRef: "work_fixture", SourceGeneration: 3, Attempt: 5, ClaimGeneration: 7, Claimant: "worker_fixture", Fence: "source_fence", ExpiresAt: test.want}
			writeback := writeBackLease(&cp.ManagedConfigurationGitWriteBackLease{ProposalRef: "proposal_fixture", Attempt: 11, ClaimGeneration: 13, Claimant: "writer_fixture", Fence: "write_fence", ExpiresAt: test.input})
			wantWriteback := entity.ConfigurationWriteBackLease{ProposalRef: "proposal_fixture", Attempt: 11, ClaimGeneration: 13, Claimant: "writer_fixture", Fence: "write_fence", ExpiresAt: test.want}
			if !reflect.DeepEqual(source, wantSource) || !reflect.DeepEqual(writeback, wantWriteback) {
				t.Fatal("typed lease pins or expiry semantics changed")
			}
		})
	}
	if !reflect.DeepEqual(sourceLease(nil), entity.ManagedConfigurationSourceLease{}) || !reflect.DeepEqual(writeBackLease(nil), entity.ConfigurationWriteBackLease{}) {
		t.Fatal("nil lease semantics changed")
	}
}

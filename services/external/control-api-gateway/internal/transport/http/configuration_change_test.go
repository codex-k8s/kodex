package httptransport

import (
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestConfigurationChangeCasterIncludesManagedOwnershipActions(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"detach_access_configuration", "copy_access_configuration"} {
		t.Run(action, func(t *testing.T) {
			input := &controlplanev1.AuditEvent{
				Id: uuid.NewString(), Action: action, ResourceId: uuid.NewString(),
				ResourceKind:    controlplanev1.ResourceKind_RESOURCE_KIND_TEAM,
				ResourceVersion: 2, Outcome: "succeeded", ActorId: uuid.NewString(),
				CorrelationId: uuid.NewString(), PolicyRevision: 1,
				OccurredAt: timestamppb.New(time.Now().UTC()),
			}
			converted, err := ConvertConfigurationChange(input)
			if err != nil || string(converted.Action) != action {
				t.Fatalf("managed ownership action must remain visible: action=%q err=%v", converted.Action, err)
			}
		})
	}
}

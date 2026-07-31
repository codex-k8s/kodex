// Package event задаёт service-owned факты до сериализации общего envelope.
package event

import (
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
)

const (
	RuntimeConfigurationChanged = "control_plane.runtime_configuration_changed"
	ScheduleChanged             = "control_plane.schedule_changed"
)

// Change — безопасная ссылка для авторитетного чтения потребителем.
type Change struct {
	EventID         string
	EventName       string
	OrganizationID  string
	ProjectID       string
	ResourceID      string
	ResourceKind    enum.Kind
	ResourceState   enum.State
	ResourceVersion uint64
	EventSequence   uint64
	OccurredAt      time.Time
	CorrelationID   string
	CausationID     string
}

// EventNameForKind возвращает только утверждённые факты с потребителями.
func EventNameForKind(kind enum.Kind) (string, bool) {
	switch kind {
	case enum.KindProject, enum.KindTeam, enum.KindChat, enum.KindRole,
		enum.KindPromptProfile, enum.KindCredentialBinding,
		enum.KindRepositoryWorkspace, enum.KindIntegration,
		enum.KindRuntimeRevision, enum.KindSession, enum.KindTurn:
		return RuntimeConfigurationChanged, true
	case enum.KindSchedule:
		return ScheduleChanged, true
	default:
		return "", false
	}
}

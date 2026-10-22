package entity

import "time"

// ArtifactBindingTarget describes one agent's current artifact binding and independent bind and unbind
// eligibility reasons at an exact agent version. Presentation flags are not authority to change a
// binding after the snapshot becomes stale.
// ArtifactBindingTarget описывает текущую привязку материала сотруднику и отдельные причины
// допустимости привязки и снятия на точной версии сотрудника. Признаки представления не разрешают
// менять связь после устаревания снимка.
type ArtifactBindingTarget struct {
	AgentRef, Name, State, BindReason, UnbindReason string
	AgentVersion                                    int64
	Bound, CanBind, CanUnbind                       bool
}

// ArtifactBindingTargets returns a paged binding-target evaluation pinned to artifact, project,
// digest, aggregate version, and evaluation time. The owner rechecks resource and consumer versions
// before applying a selected change.
// ArtifactBindingTargets возвращает постраничную оценку целей привязки с привязками материала,
// проекта, дайджеста, версии агрегата и времени. Перед применением выбора владелец повторно проверяет
// версии ресурса и потребителя.
type ArtifactBindingTargets struct {
	ArtifactRef, ProjectRef, Digest, NextPageToken string
	ArtifactVersion, Total                         int64
	Items                                          []ArtifactBindingTarget
	EvaluatedAt                                    time.Time
}

// RunAttachmentEligibility records whether an artifact attachment is eligible for a pinned run target
// and workflow revision, including reason, run version, digest, and evaluation time. It is selection
// evidence rather than an attachment mutation or a runtime grant.
// RunAttachmentEligibility фиксирует допустимость вложения материала для закреплённых цели запуска и
// ревизии процесса с причиной, версией запуска, дайджестом и временем. Это свидетельство выбора, а не
// изменение вложений или разрешение исполнения.
type RunAttachmentEligibility struct {
	ProjectRef, RunRef, WorkflowVersionRef, Reason, Digest string
	Target                                                 RunTarget
	RunVersion                                             int64
	Eligible                                               bool
	EvaluatedAt                                            time.Time
}

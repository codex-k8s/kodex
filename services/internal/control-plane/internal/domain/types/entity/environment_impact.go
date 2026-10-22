package entity

// RuntimeEnvironmentConsumer identifies an agent's exact environment binding and version with project
// or organization scope and concurrency versions. It describes a resolved consumer and does not
// authorize rewriting another agent's binding.
// RuntimeEnvironmentConsumer определяет точные привязку и версию окружения сотрудника с областью
// проекта или организации и версиями конкуренции. Это описание разрешённого потребителя, а не право
// переписывать привязку другого сотрудника.
type RuntimeEnvironmentConsumer struct {
	AgentRef, BindingRef, VersionRef, ProjectRef string
	ScopeKind, OrganizationRef                   string
	AgentVersion, BindingVersion                 int64
}

// RuntimeEnvironmentImpact returns consumers affected by a selected environment target version and
// digest, together with aggregate version, total, and pagination. Reading the impact does not publish
// the target or apply consumer changes.
// RuntimeEnvironmentImpact возвращает потребителей, затронутых выбранными целевой версией и дайджестом
// окружения, с версией агрегата, количеством и пагинацией. Чтение влияния не публикует цель и не
// применяет изменения потребителей.
type RuntimeEnvironmentImpact struct {
	EnvironmentRef, TargetVersionRef, TargetDigest, NextPageToken string
	EnvironmentVersion, Total                                     int64
	Consumers                                                     []RuntimeEnvironmentConsumer
}

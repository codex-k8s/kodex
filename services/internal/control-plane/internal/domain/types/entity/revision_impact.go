package entity

import "time"

// RevisionImpactPlan pins a managed source and draft version to a target digest, impact digest, state,
// lifetime, and optional published revision. Its identity and versions define the exact plan; they are
// not approval to mutate every consumer.
// RevisionImpactPlan закрепляет версии управляемого источника и черновика за целевым дайджестом,
// дайджестом влияния, состоянием, сроком и необязательной опубликованной ревизией. Личность и версии
// определяют точный план, но не подтверждают изменение каждого потребителя.
type RevisionImpactPlan struct {
	Ref, Kind, SourceRef, SourceRevisionRef, DraftRef string
	Version, SourceVersion, DraftVersion              int64
	TargetDigest, Digest, State, PublishedRevisionRef string
	Total                                             int64
	CreatedAt, ExpiresAt                              time.Time
}

// RevisionImpactItem records one scoped consumer's source revision and binding versions, outcome, and
// resulting revision and binding pins. These fields support per-item progress and replay checks
// without implying a single atomic transition across all consumers.
// RevisionImpactItem фиксирует исходную ревизию и версии привязки потребителя в области владельца,
// исход и итоговые привязки ревизии и связи. Поля поддерживают ход по элементам и проверки повтора, не
// подразумевая единого атомарного перехода всех потребителей.
type RevisionImpactItem struct {
	Ref, ProjectRef, ConsumerKind, ConsumerRef  string
	ScopeKind, OrganizationRef                  string
	ConsumerVersion                             int64
	BindingRef                                  string
	BindingVersion                              int64
	SourceRevisionRef, Outcome                  string
	ResultRevisionRef, ResultBindingRef         string
	ResultBindingVersion, ResultConsumerVersion int64
}

// RevisionImpactPage returns one page of consumer impact results with its pinned plan, total, and
// opaque continuation token. The owner must retain the same plan scope and concurrency pins when
// continuing the read.
// RevisionImpactPage возвращает страницу результатов влияния на потребителей с закреплённым планом,
// количеством и непрозрачным токеном продолжения. При продолжении чтения владелец сохраняет область и
// привязки конкуренции того же плана.
type RevisionImpactPage struct {
	Plan          RevisionImpactPlan
	Items         []RevisionImpactItem
	Total         int64
	NextPageToken string
}

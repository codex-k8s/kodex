package entity

import "time"

// RoleImageImpactPlan pins a managed revision and admitted artifact to a recipe generation, policy
// digest, configuration version, impact digest, and expiry. Its total describes affected consumers,
// not automatic approval of every rebinding.
// RoleImageImpactPlan закрепляет управляемую ревизию и допущенный материал за поколением рецепта,
// дайджестом политики, версией конфигурации, дайджестом влияния и сроком. Общее количество описывает
// затронутых потребителей, а не автоматически подтверждает каждую перепривязку.
type RoleImageImpactPlan struct {
	Ref, ConfigurationRef, RevisionRef, RevisionDigest                      string
	RecipeRef, BuildRef, ArtifactRef, ArtifactDigest, AdmissionPolicyDigest string
	Digest, State                                                           string
	Version, ConfigurationVersion, RecipeGeneration, Total                  int64
	CreatedAt, ExpiresAt                                                    time.Time
}

// RoleImageImpactItem identifies an exact source environment version and consumer and records the
// individual outcome and resulting environment and binding pins. Per-item results preserve partial
// progress rather than asserting that the entire plan was atomic.
// RoleImageImpactItem определяет точные исходную версию окружения и потребителя и фиксирует отдельный
// исход и итоговые привязки окружения и связи. Результаты элементов сохраняют частичный ход, а не
// утверждают атомарность всего плана.
type RoleImageImpactItem struct {
	Ref, EnvironmentRef, SourceVersionRef, SourceVersionDigest string
	EnvironmentVersion                                         int64
	Consumer                                                   RuntimeEnvironmentConsumer
	Outcome, ResultEnvironmentVersionRef, ResultBindingRef     string
	ResultBindingVersion                                       int64
}

// RoleImageImpactPage returns impact items together with their pinned plan, total, and opaque
// continuation token. Pagination must retain the plan's owner scope and version; it does not expand
// eligibility.
// RoleImageImpactPage возвращает элементы влияния вместе с закреплённым планом, общим количеством и
// непрозрачным токеном продолжения. Пагинация сохраняет область владельца и версию плана и не
// расширяет допустимость.
type RoleImageImpactPage struct {
	Plan          RoleImageImpactPlan
	Items         []RoleImageImpactItem
	Total         int64
	NextPageToken string
}

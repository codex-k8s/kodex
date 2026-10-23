package entity

// RuntimeSecretImpactConsumer identifies an affected environment version and runtime consumer with
// owner scope and referenced secret revisions. It is an impact read model, not permission to rebind a
// different environment or consumer.
// RuntimeSecretImpactConsumer определяет затронутую версию окружения и потребителя исполнения с
// областью владельца и используемыми ревизиями секрета. Это модель чтения влияния, а не разрешение
// перепривязать другое окружение или потребителя.
type RuntimeSecretImpactConsumer struct {
	EnvironmentRef, EnvironmentVersionRef  string
	ScopeKind, OrganizationRef, ProjectRef string
	EnvironmentVersion                     int64
	SecretRevisions                        []int64
	Consumer                               RuntimeEnvironmentConsumer
}

// RuntimeSecretImpact returns affected consumers for one secret version and target revision with
// pagination. The target revision is a selection pin; the view itself neither publishes the secret nor
// applies rebinding.
// RuntimeSecretImpact возвращает затронутых потребителей одной версии секрета и целевой ревизии с
// пагинацией. Целевая ревизия является привязкой выбора; само представление не публикует секрет и не
// применяет перепривязку.
type RuntimeSecretImpact struct {
	SecretRef                            string
	SecretVersion, TargetRevision, Total int64
	Consumers                            []RuntimeSecretImpactConsumer
	NextPageToken                        string
}

// RuntimeSecretRebindSelection selects consumers of an exact source environment version within an
// owner scope, using an expected environment version for concurrency checks. The owner validates the
// full selection before applying any binding change.
// RuntimeSecretRebindSelection выбирает потребителей точной исходной версии окружения в области
// владельца с ожидаемой версией окружения для проверки конкуренции. До применения изменения привязки
// владелец проверяет весь выбор.
type RuntimeSecretRebindSelection struct {
	EnvironmentRef, SourceVersionRef       string
	ScopeKind, OrganizationRef, ProjectRef string
	ExpectedEnvironmentVersion             int64
	Consumers                              []RuntimeEnvironmentConsumer
}

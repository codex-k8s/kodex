package entity

import "time"

// RuntimeSecretDraftImpactPlan pins a versioned draft and secret to a source revision, impact digest,
// state, count, and expiry. The plan describes the exact publication impact under consideration rather
// than approving every listed consumer.
// RuntimeSecretDraftImpactPlan закрепляет версионированные черновик и секрет за исходной ревизией,
// дайджестом влияния, состоянием, количеством и сроком. План описывает точное рассматриваемое влияние
// публикации, а не подтверждает каждого перечисленного потребителя.
type RuntimeSecretDraftImpactPlan struct {
	Ref, DraftRef, SecretRef, Digest, State            string
	DraftVersion, SecretVersion, SourceRevision, Total int64
	ExpiresAt                                          time.Time
}

// RuntimeSecretDraftImpactItem pairs one affected secret consumer with its recorded outcome and
// resulting environment and binding pins. These result references preserve per-consumer progress
// rather than implying atomic success of the entire impact plan.
// RuntimeSecretDraftImpactItem связывает затронутого потребителя секрета с зафиксированным исходом и
// итоговыми привязками окружения и связи. Результирующие ссылки сохраняют ход по каждому потребителю,
// а не подразумевают атомарный успех всего плана влияния.
type RuntimeSecretDraftImpactItem struct {
	Ref, Outcome, ResultEnvironmentVersionRef, ResultBindingRef string
	ResultBindingVersion                                        int64
	Consumer                                                    RuntimeSecretImpactConsumer
}

// RuntimeSecretDraftImpactPage returns a page of impact items with their pinned plan, total, and
// opaque continuation token. Following the token requires resolving that same owner-scoped plan and
// version.
// RuntimeSecretDraftImpactPage возвращает страницу элементов влияния с закреплённым планом, общим
// количеством и непрозрачным токеном продолжения. Использование токена требует разрешения того же
// плана и версии в области владельца.
type RuntimeSecretDraftImpactPage struct {
	Plan          RuntimeSecretDraftImpactPlan
	Items         []RuntimeSecretDraftImpactItem
	Total         int64
	NextPageToken string
}

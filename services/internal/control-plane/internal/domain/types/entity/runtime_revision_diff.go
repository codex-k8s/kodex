package entity

import "time"

// PublicRuntimeRevisionIdentity exposes revision, run, session, turn, attempt, version, digest, and
// creation time without sharing the private worker-snapshot type. These public identifiers do not
// confer worker execution authority.
// PublicRuntimeRevisionIdentity раскрывает ревизию, запуск, сессию, ход, попытку, версию, дайджест и
// время создания без общего типа с закрытым снимком исполнителя. Публичные идентификаторы не
// предоставляют исполнителю полномочия.
type PublicRuntimeRevisionIdentity struct {
	Ref, RunRef, SessionRef, TurnRef, RevisionDigest string
	Version                                          int64
	Attempt                                          int32
	CreatedAt                                        time.Time
}

// RuntimeRevisionDiffValue is the public reference, version, digest, and revision metadata for one
// runtime component. It deliberately does not carry the component's private snapshot or credential
// bytes.
// RuntimeRevisionDiffValue является публичными метаданными ссылки, версии, дайджеста и ревизии
// компонента исполнения. Структура намеренно не передаёт закрытый снимок компонента или байты
// реквизитов.
type RuntimeRevisionDiffValue struct {
	Ref      string
	Version  int64
	Digest   string
	Revision string
}

// RuntimeRevisionDiffChange pairs optional previous and current public metadata for a named component.
// A nil previous value can represent the absence of a comparison revision; it is not a continuation
// command or a request to mutate the component.
// RuntimeRevisionDiffChange связывает необязательные прежние и текущие публичные метаданные
// именованного компонента. Nil прежнего значения может обозначать отсутствие ревизии для сравнения;
// это не команда продолжения и не запрос изменения компонента.
type RuntimeRevisionDiffChange struct {
	Component         string
	Previous, Current *RuntimeRevisionDiffValue
}

// RuntimeRevisionDiff presents current and optional previous public revision identities with component
// changes for inspection. It is distinct from the sealed continuation-notice diff and cannot authorize
// a turn or reveal a private worker snapshot.
// RuntimeRevisionDiff представляет текущую и необязательную прежнюю публичные личности ревизий с
// изменениями компонентов для просмотра. Это не различия заверенного уведомления продолжения;
// структура не разрешает ход и не раскрывает закрытый снимок исполнителя.
type RuntimeRevisionDiff struct {
	Current  PublicRuntimeRevisionIdentity
	Previous *PublicRuntimeRevisionIdentity
	Changes  []RuntimeRevisionDiffChange
}

// RuntimeRevisionPublicProjection contains the closed set of public component metadata used by the
// repository's dedicated read path, which does not read worker snapshot JSON for this operation. It
// must remain separate from the private execution projection.
// RuntimeRevisionPublicProjection содержит закрытый набор публичных метаданных компонентов для
// отдельного чтения репозитория, не читающего JSON снимка исполнителя в этой операции. Представление
// остаётся отдельным от закрытой проекции исполнения.
type RuntimeRevisionPublicProjection struct {
	Identity PublicRuntimeRevisionIdentity
	Provider, Model, RuntimeProfile, RuntimeConfiguration, ProviderPolicy,
	ConfigOverlay, Environment, EnvironmentBinding, Instruction,
	IntegrationGrants, Image RuntimeRevisionDiffValue
}

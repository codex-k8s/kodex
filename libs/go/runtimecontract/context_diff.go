package runtimecontract

// RuntimeContextDescriptor is the shared JSON shape of a context component value: reference,
// version, digest, and a bounded descriptive value. Its fields omit zero values; producers and
// consumers separately validate bounds, provenance, and whether a component may be disclosed.
// RuntimeContextDescriptor задаёт общую JSON-форму значения компонента контекста: ссылку, версию,
// дайджест и ограниченное описательное значение. Нулевые поля опускаются; производитель и потребитель
// отдельно проверяют пределы, происхождение и допустимость раскрытия компонента.
type RuntimeContextDescriptor struct {
	Ref     string `json:"ref,omitempty"`
	Version int64  `json:"version,omitempty"`
	Digest  string `json:"digest,omitempty"`
	Value   string `json:"value,omitempty"`
}

// RuntimeContextChange is the shared JSON shape of one ordered context transition. Descriptor lets
// each boundary retain its distinct named descriptor type. Nil and empty slices remain distinct
// on the wire; the shape does not enforce component order, supported actions, or descriptor validity.
// RuntimeContextChange задаёт общую JSON-форму одного упорядоченного изменения контекста. Descriptor
// позволяет каждой границе сохранить отдельный именованный тип дескриптора. Nil и пустые срезы
// различаются в передаче; форма не проверяет порядок компонентов, допустимые действия и дескрипторы.
type RuntimeContextChange[Descriptor any] struct {
	Component string       `json:"component"`
	Previous  []Descriptor `json:"previous"`
	Current   []Descriptor `json:"current"`
	Action    string       `json:"action"`
}

// RuntimeContextDiff is the shared JSON shape binding ordered changes to revision, session, turn,
// and attempt identifiers. Change preserves each boundary's named transition type. Digest inclusion,
// canonical calculation, exact identity checks, and prospective-versus-execution rules belong to
// the existing producer and consumer, not to this declaration.
// RuntimeContextDiff задаёт общую JSON-форму привязки упорядоченных изменений к ревизиям, сессии, ходу
// и попытке. Change сохраняет именованный тип перехода каждой границы. Включение дайджеста, его
// канонический расчёт, точные проверки личности и различие предварительной и исполняемой форм
// принадлежат существующим производителю и потребителю, а не этому объявлению.
type RuntimeContextDiff[Change any] struct {
	PreviousRevisionRef string   `json:"previousRevisionRef"`
	CurrentRevisionRef  string   `json:"currentRevisionRef"`
	SessionRef          string   `json:"sessionRef"`
	TurnRef             string   `json:"turnRef"`
	Attempt             int32    `json:"attempt"`
	Changes             []Change `json:"changes"`
	Digest              string   `json:"digest"`
}

package entity

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"time"
)

// RuntimeEnvironmentDraftSpecification carries editable environment metadata, a selected image
// artifact, variables, secret bindings, tools, and runtime policy. Secret bindings are references
// rather than values; the owner validates every dependency before publication.
// RuntimeEnvironmentDraftSpecification передаёт редактируемые метаданные окружения, выбранный материал
// образа, переменные, привязки секретов, инструменты и политику исполнения. Привязки секретов являются
// ссылками, а не значениями; перед публикацией владелец проверяет каждую зависимость.
type RuntimeEnvironmentDraftSpecification struct {
	Name, Description, ImageArtifactRef string
	Values                              []RuntimeEnvironmentValue
	SecretBindings                      []RuntimeSecretBinding
	Tools                               []RuntimeEnvironmentTool
	Policy                              runtimecontract.RuntimeEnvironmentPolicy
}

// RuntimeEnvironmentDraft presents the scoped editable draft with base revision, expected environment
// version, validation digest, state, diagnostics, and publication reference. SavedAt indicates
// persisted editing, not successful validation or publication.
// RuntimeEnvironmentDraft представляет редактируемый черновик в области владельца с базовой ревизией,
// ожидаемой версией окружения, дайджестом проверки, состоянием, диагностикой и ссылкой публикации.
// SavedAt обозначает сохранение правок, а не успешную проверку или публикацию.
type RuntimeEnvironmentDraft struct {
	ScopeKind, OrganizationRef                                                        string
	BaseVersionRef                                                                    string
	BaseRevision                                                                      int64
	SavedAt                                                                           time.Time
	Ref, ProjectRef, EnvironmentRef, State, ValidationDigest, PublishedEnvironmentRef string
	Version, ExpectedEnvironmentVersion                                               int64
	Specification                                                                     RuntimeEnvironmentDraftSpecification
	Diagnostics                                                                       []string
}

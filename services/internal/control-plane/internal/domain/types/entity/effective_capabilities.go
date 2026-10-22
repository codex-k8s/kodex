package entity

import "time"

// EffectiveCapability describes a capability's requested, required, effective, and grantable states
// with reason, source, and exact connection and grant versions. The projection explains a computed
// decision; it is not a transferable grant token.
// EffectiveCapability описывает запрошенное, обязательное, фактическое и допустимое к выдаче состояния
// возможности с причиной, источником и точными версиями подключения и разрешения. Проекция объясняет
// рассчитанное решение, а не является передаваемым токеном разрешения.
type EffectiveCapability struct {
	Key, Name, Description, Source, Reason    string
	Requested, Required, Effective, Grantable bool
	ConnectionRef, GrantRef, DefinitionDigest string
	ConnectionVersion, GrantVersion           int64
}

// AgentEffectiveCapabilities returns a paged capability evaluation bound to an agent, runtime
// configuration, environment, optional workflow step, versions, digest, and time. RuntimeReady
// describes this evaluation and does not replace fresh admission when claiming execution.
// AgentEffectiveCapabilities возвращает постраничную оценку возможностей с привязками сотрудника,
// конфигурации исполнения, окружения, необязательного этапа процесса, версий, дайджеста и времени.
// RuntimeReady описывает эту оценку и не заменяет свежий допуск при захвате исполнения.
type AgentEffectiveCapabilities struct {
	AgentRef, ProjectRef, RuntimeConfigurationRef, EnvironmentVersionRef string
	WorkflowRef, WorkflowVersionRef, StepKey, Digest                     string
	AgentVersion, RuntimeConfigurationVersion                            int64
	RuntimeReady                                                         bool
	EvaluatedAt                                                          time.Time
	Items                                                                []EffectiveCapability
	Total                                                                int64
	NextPageToken                                                        string
}

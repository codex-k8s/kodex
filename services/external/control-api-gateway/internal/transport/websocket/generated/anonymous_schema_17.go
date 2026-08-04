package generated

import (
	"encoding/json"
)

type AnonymousSchema_17 uint

const (
	AnonymousSchema_17Project AnonymousSchema_17 = iota
	AnonymousSchema_17Team
	AnonymousSchema_17Chat
	AnonymousSchema_17Role
	AnonymousSchema_17PromptProfile
	AnonymousSchema_17CredentialBinding
	AnonymousSchema_17RepositoryWorkspace
	AnonymousSchema_17Integration
	AnonymousSchema_17RuntimeRevision
	AnonymousSchema_17Session
	AnonymousSchema_17Turn
	AnonymousSchema_17ProcessRun
	AnonymousSchema_17Schedule
	AnonymousSchema_17OwnerGate
	AnonymousSchema_17MemoryRecord
	AnonymousSchema_17WorkClaim
	AnonymousSchema_17Artifact
)

// Value returns the value of the enum.
func (op AnonymousSchema_17) Value() any {
	if op >= AnonymousSchema_17(len(AnonymousSchema_17Values)) {
		return nil
	}
	return AnonymousSchema_17Values[op]
}

var AnonymousSchema_17Values = []any{"PROJECT", "TEAM", "CHAT", "ROLE", "PROMPT_PROFILE", "CREDENTIAL_BINDING", "REPOSITORY_WORKSPACE", "INTEGRATION", "RUNTIME_REVISION", "SESSION", "TURN", "PROCESS_RUN", "SCHEDULE", "OWNER_GATE", "MEMORY_RECORD", "WORK_CLAIM", "ARTIFACT"}
var ValuesToAnonymousSchema_17 = map[any]AnonymousSchema_17{
	AnonymousSchema_17Values[AnonymousSchema_17Project]:             AnonymousSchema_17Project,
	AnonymousSchema_17Values[AnonymousSchema_17Team]:                AnonymousSchema_17Team,
	AnonymousSchema_17Values[AnonymousSchema_17Chat]:                AnonymousSchema_17Chat,
	AnonymousSchema_17Values[AnonymousSchema_17Role]:                AnonymousSchema_17Role,
	AnonymousSchema_17Values[AnonymousSchema_17PromptProfile]:       AnonymousSchema_17PromptProfile,
	AnonymousSchema_17Values[AnonymousSchema_17CredentialBinding]:   AnonymousSchema_17CredentialBinding,
	AnonymousSchema_17Values[AnonymousSchema_17RepositoryWorkspace]: AnonymousSchema_17RepositoryWorkspace,
	AnonymousSchema_17Values[AnonymousSchema_17Integration]:         AnonymousSchema_17Integration,
	AnonymousSchema_17Values[AnonymousSchema_17RuntimeRevision]:     AnonymousSchema_17RuntimeRevision,
	AnonymousSchema_17Values[AnonymousSchema_17Session]:             AnonymousSchema_17Session,
	AnonymousSchema_17Values[AnonymousSchema_17Turn]:                AnonymousSchema_17Turn,
	AnonymousSchema_17Values[AnonymousSchema_17ProcessRun]:          AnonymousSchema_17ProcessRun,
	AnonymousSchema_17Values[AnonymousSchema_17Schedule]:            AnonymousSchema_17Schedule,
	AnonymousSchema_17Values[AnonymousSchema_17OwnerGate]:           AnonymousSchema_17OwnerGate,
	AnonymousSchema_17Values[AnonymousSchema_17MemoryRecord]:        AnonymousSchema_17MemoryRecord,
	AnonymousSchema_17Values[AnonymousSchema_17WorkClaim]:           AnonymousSchema_17WorkClaim,
	AnonymousSchema_17Values[AnonymousSchema_17Artifact]:            AnonymousSchema_17Artifact,
}

func (op *AnonymousSchema_17) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_17[v]
	return nil
}

func (op AnonymousSchema_17) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}

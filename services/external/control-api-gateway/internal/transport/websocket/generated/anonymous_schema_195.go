package generated

import (
	"encoding/json"
)

type AnonymousSchema_195 uint

const (
	AnonymousSchema_195Project AnonymousSchema_195 = iota
	AnonymousSchema_195Team
	AnonymousSchema_195Chat
	AnonymousSchema_195Role
	AnonymousSchema_195PromptProfile
	AnonymousSchema_195CredentialBinding
	AnonymousSchema_195RepositoryWorkspace
	AnonymousSchema_195Integration
	AnonymousSchema_195RuntimeRevision
	AnonymousSchema_195Session
	AnonymousSchema_195Turn
	AnonymousSchema_195ProcessRun
	AnonymousSchema_195Schedule
	AnonymousSchema_195OwnerGate
	AnonymousSchema_195MemoryRecord
	AnonymousSchema_195WorkClaim
	AnonymousSchema_195Artifact
)

// Value returns the value of the enum.
func (op AnonymousSchema_195) Value() any {
	if op >= AnonymousSchema_195(len(AnonymousSchema_195Values)) {
		return nil
	}
	return AnonymousSchema_195Values[op]
}

var AnonymousSchema_195Values = []any{"PROJECT", "TEAM", "CHAT", "ROLE", "PROMPT_PROFILE", "CREDENTIAL_BINDING", "REPOSITORY_WORKSPACE", "INTEGRATION", "RUNTIME_REVISION", "SESSION", "TURN", "PROCESS_RUN", "SCHEDULE", "OWNER_GATE", "MEMORY_RECORD", "WORK_CLAIM", "ARTIFACT"}
var ValuesToAnonymousSchema_195 = map[any]AnonymousSchema_195{
	AnonymousSchema_195Values[AnonymousSchema_195Project]:             AnonymousSchema_195Project,
	AnonymousSchema_195Values[AnonymousSchema_195Team]:                AnonymousSchema_195Team,
	AnonymousSchema_195Values[AnonymousSchema_195Chat]:                AnonymousSchema_195Chat,
	AnonymousSchema_195Values[AnonymousSchema_195Role]:                AnonymousSchema_195Role,
	AnonymousSchema_195Values[AnonymousSchema_195PromptProfile]:       AnonymousSchema_195PromptProfile,
	AnonymousSchema_195Values[AnonymousSchema_195CredentialBinding]:   AnonymousSchema_195CredentialBinding,
	AnonymousSchema_195Values[AnonymousSchema_195RepositoryWorkspace]: AnonymousSchema_195RepositoryWorkspace,
	AnonymousSchema_195Values[AnonymousSchema_195Integration]:         AnonymousSchema_195Integration,
	AnonymousSchema_195Values[AnonymousSchema_195RuntimeRevision]:     AnonymousSchema_195RuntimeRevision,
	AnonymousSchema_195Values[AnonymousSchema_195Session]:             AnonymousSchema_195Session,
	AnonymousSchema_195Values[AnonymousSchema_195Turn]:                AnonymousSchema_195Turn,
	AnonymousSchema_195Values[AnonymousSchema_195ProcessRun]:          AnonymousSchema_195ProcessRun,
	AnonymousSchema_195Values[AnonymousSchema_195Schedule]:            AnonymousSchema_195Schedule,
	AnonymousSchema_195Values[AnonymousSchema_195OwnerGate]:           AnonymousSchema_195OwnerGate,
	AnonymousSchema_195Values[AnonymousSchema_195MemoryRecord]:        AnonymousSchema_195MemoryRecord,
	AnonymousSchema_195Values[AnonymousSchema_195WorkClaim]:           AnonymousSchema_195WorkClaim,
	AnonymousSchema_195Values[AnonymousSchema_195Artifact]:            AnonymousSchema_195Artifact,
}

func (op *AnonymousSchema_195) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_195[v]
	return nil
}

func (op AnonymousSchema_195) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}

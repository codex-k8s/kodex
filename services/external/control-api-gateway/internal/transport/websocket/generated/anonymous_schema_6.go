package generated

import (
	"encoding/json"
)

type AnonymousSchema_6 uint

const (
	AnonymousSchema_6Project AnonymousSchema_6 = iota
	AnonymousSchema_6Team
	AnonymousSchema_6Chat
	AnonymousSchema_6Role
	AnonymousSchema_6PromptProfile
	AnonymousSchema_6CredentialBinding
	AnonymousSchema_6RepositoryWorkspace
	AnonymousSchema_6Integration
	AnonymousSchema_6RuntimeRevision
	AnonymousSchema_6Session
	AnonymousSchema_6Turn
	AnonymousSchema_6ProcessRun
	AnonymousSchema_6Schedule
	AnonymousSchema_6OwnerGate
	AnonymousSchema_6MemoryRecord
	AnonymousSchema_6WorkClaim
	AnonymousSchema_6Artifact
)

// Value returns the value of the enum.
func (op AnonymousSchema_6) Value() any {
	if op >= AnonymousSchema_6(len(AnonymousSchema_6Values)) {
		return nil
	}
	return AnonymousSchema_6Values[op]
}

var AnonymousSchema_6Values = []any{"PROJECT", "TEAM", "CHAT", "ROLE", "PROMPT_PROFILE", "CREDENTIAL_BINDING", "REPOSITORY_WORKSPACE", "INTEGRATION", "RUNTIME_REVISION", "SESSION", "TURN", "PROCESS_RUN", "SCHEDULE", "OWNER_GATE", "MEMORY_RECORD", "WORK_CLAIM", "ARTIFACT"}
var ValuesToAnonymousSchema_6 = map[any]AnonymousSchema_6{
	AnonymousSchema_6Values[AnonymousSchema_6Project]:             AnonymousSchema_6Project,
	AnonymousSchema_6Values[AnonymousSchema_6Team]:                AnonymousSchema_6Team,
	AnonymousSchema_6Values[AnonymousSchema_6Chat]:                AnonymousSchema_6Chat,
	AnonymousSchema_6Values[AnonymousSchema_6Role]:                AnonymousSchema_6Role,
	AnonymousSchema_6Values[AnonymousSchema_6PromptProfile]:       AnonymousSchema_6PromptProfile,
	AnonymousSchema_6Values[AnonymousSchema_6CredentialBinding]:   AnonymousSchema_6CredentialBinding,
	AnonymousSchema_6Values[AnonymousSchema_6RepositoryWorkspace]: AnonymousSchema_6RepositoryWorkspace,
	AnonymousSchema_6Values[AnonymousSchema_6Integration]:         AnonymousSchema_6Integration,
	AnonymousSchema_6Values[AnonymousSchema_6RuntimeRevision]:     AnonymousSchema_6RuntimeRevision,
	AnonymousSchema_6Values[AnonymousSchema_6Session]:             AnonymousSchema_6Session,
	AnonymousSchema_6Values[AnonymousSchema_6Turn]:                AnonymousSchema_6Turn,
	AnonymousSchema_6Values[AnonymousSchema_6ProcessRun]:          AnonymousSchema_6ProcessRun,
	AnonymousSchema_6Values[AnonymousSchema_6Schedule]:            AnonymousSchema_6Schedule,
	AnonymousSchema_6Values[AnonymousSchema_6OwnerGate]:           AnonymousSchema_6OwnerGate,
	AnonymousSchema_6Values[AnonymousSchema_6MemoryRecord]:        AnonymousSchema_6MemoryRecord,
	AnonymousSchema_6Values[AnonymousSchema_6WorkClaim]:           AnonymousSchema_6WorkClaim,
	AnonymousSchema_6Values[AnonymousSchema_6Artifact]:            AnonymousSchema_6Artifact,
}

func (op *AnonymousSchema_6) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_6[v]
	return nil
}

func (op AnonymousSchema_6) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}

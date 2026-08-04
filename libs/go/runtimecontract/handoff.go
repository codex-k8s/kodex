// Package runtimecontract содержит узкие канонические wire DTO между
// agent-runner и runtime-controller. В package нет transport или domain policy.
package runtimecontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"
)

const HandoffSchemaV1 = "mattercodex.runtime-turn-handoff.v1"

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// HandoffV1 — exact terminal handoff одной immutable execution authority.
type HandoffV1 struct {
	Schema                  string    `json:"schema"`
	ExecutionID             string    `json:"execution_id"`
	ExecutionVersion        uint64    `json:"execution_version"`
	Fence                   uint64    `json:"fence"`
	GrantGeneration         uint64    `json:"grant_generation"`
	RuntimeRevisionSHA256   string    `json:"runtime_revision_sha256"`
	EffectiveRuntimeSHA256  string    `json:"effective_runtime_sha256"`
	ImmutableInputSHA256    string    `json:"immutable_input_sha256"`
	AgentSessionID          int64     `json:"agent_session_id"`
	AgentSessionTurnID      int64     `json:"agent_session_turn_id"`
	AgentRunID              string    `json:"agent_run_id"`
	AgentBindingSHA256      string    `json:"agent_binding_sha256"`
	Outcome                 string    `json:"outcome"`
	TerminalReference       string    `json:"terminal_reference"`
	TerminalSHA256          string    `json:"terminal_sha256"`
	ResultArtifactID        string    `json:"result_artifact_id"`
	ResultArtifactVersion   uint64    `json:"result_artifact_version"`
	ResultArtifactSHA256    string    `json:"result_artifact_sha256"`
	ResultArtifactName      string    `json:"result_artifact_name"`
	ResultArtifactMediaType string    `json:"result_artifact_media_type"`
	ResultArtifactPayload   []byte    `json:"result_artifact_payload"`
	ObservedAt              time.Time `json:"observed_at"`
}

func (handoff HandoffV1) Validate() error {
	resultDigest := sha256.Sum256(handoff.ResultArtifactPayload)
	if handoff.Schema != HandoffSchemaV1 || handoff.ExecutionID == "" ||
		handoff.ExecutionVersion == 0 || handoff.Fence == 0 || handoff.GrantGeneration == 0 ||
		!sha256Pattern.MatchString(handoff.RuntimeRevisionSHA256) ||
		!sha256Pattern.MatchString(handoff.EffectiveRuntimeSHA256) ||
		!sha256Pattern.MatchString(handoff.ImmutableInputSHA256) ||
		handoff.AgentSessionID <= 0 || handoff.AgentSessionTurnID <= 0 || handoff.AgentRunID == "" ||
		!sha256Pattern.MatchString(handoff.AgentBindingSHA256) || handoff.Outcome == "" ||
		handoff.TerminalReference == "" || !sha256Pattern.MatchString(handoff.TerminalSHA256) ||
		handoff.ResultArtifactID == "" || handoff.ResultArtifactVersion == 0 ||
		!sha256Pattern.MatchString(handoff.ResultArtifactSHA256) ||
		handoff.ResultArtifactSHA256 != hex.EncodeToString(resultDigest[:]) || handoff.ResultArtifactName == "" ||
		handoff.ResultArtifactMediaType != "text/markdown" || len(handoff.ResultArtifactPayload) == 0 ||
		len(handoff.ResultArtifactPayload) > 160<<10 ||
		handoff.ObservedAt.IsZero() {
		return errors.New("runtime handoff is invalid")
	}
	return nil
}

func EncodeHandoff(handoff HandoffV1) ([]byte, error) {
	if err := handoff.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(handoff)
}

func DecodeHandoff(raw []byte) (HandoffV1, error) {
	var handoff HandoffV1
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&handoff); err != nil {
		return HandoffV1{}, errors.New("decode runtime handoff")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return HandoffV1{}, errors.New("runtime handoff has trailing data")
	}
	if err := handoff.Validate(); err != nil {
		return HandoffV1{}, err
	}
	return handoff, nil
}

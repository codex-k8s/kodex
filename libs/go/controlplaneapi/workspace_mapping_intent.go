package controlplaneapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// WorkspaceMattermostMappingIntent описывает вычислимый producer-ом
// semantic intent специализированной mapping-команды. Transport receipt/JTI,
// policy revision и digest самого receipt намеренно не входят в структуру:
// иначе подпись образовала бы самоссылочный hash.
type WorkspaceMattermostMappingIntent struct {
	ActorID            string `json:"actor_id"`
	OrganizationID     string `json:"organization_id"`
	ProjectID          string `json:"project_id"`
	WorkspaceID        string `json:"workspace_id"`
	Action             string `json:"action"`
	MappingID          string `json:"mapping_id,omitempty"`
	DisplayName        string `json:"display_name,omitempty"`
	ExpectedVersion    uint64 `json:"expected_version,omitempty"`
	ExpectedGeneration uint64 `json:"expected_generation,omitempty"`
	ProviderTeamRef    string `json:"provider_team_ref"`
	ProviderObjectRef  string `json:"provider_object_ref"`
	EffectGeneration   uint64 `json:"effect_generation"`
	EffectSHA256       string `json:"effect_sha256"`
}

// WorkspaceMattermostMappingIntentSHA256 возвращает единый canonical digest,
// который используют producer и авторитетный control-plane consumer.
func WorkspaceMattermostMappingIntentSHA256(input WorkspaceMattermostMappingIntent) (string, error) {
	if !validUUID(input.ActorID) || !validUUID(input.OrganizationID) || !validUUID(input.ProjectID) ||
		!validUUID(input.WorkspaceID) || input.ProjectID != input.WorkspaceID ||
		(input.Action != "bind" && input.Action != "relink" && input.Action != "unlink") ||
		input.ProviderTeamRef == "" || len(input.ProviderTeamRef) > 64 ||
		input.ProviderObjectRef == "" || len(input.ProviderObjectRef) > 128 ||
		input.EffectGeneration == 0 || !validSHA256(input.EffectSHA256) {
		return "", errors.New("workspace Mattermost mapping intent is invalid")
	}
	if input.Action == "bind" {
		if input.MappingID != "" || input.ExpectedVersion != 0 || input.ExpectedGeneration != 0 ||
			strings.TrimSpace(input.DisplayName) == "" || len(input.DisplayName) > 256 {
			return "", errors.New("workspace Mattermost bind intent is invalid")
		}
	} else if !validUUID(input.MappingID) || input.ExpectedVersion == 0 || input.ExpectedGeneration == 0 ||
		input.DisplayName != "" {
		return "", errors.New("workspace Mattermost transition intent is invalid")
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", errors.New("encode workspace Mattermost mapping intent")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil
}

func validSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, symbol := range value {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}

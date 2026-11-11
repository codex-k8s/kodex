package opencode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	// MaximumCheckpointBytes limits a native export, independently of message-page and model-context limits.
	// MaximumCheckpointBytes ограничивает native export отдельно от страницы сообщений и контекста модели.
	MaximumCheckpointBytes = runtimecontract.MaximumSessionSourceBytes
)

var (
	// ErrCheckpoint rejects damaged or foreign exports without replacing them with an empty session.
	// ErrCheckpoint отклоняет повреждённый или чужой export без подмены пустой сессией.
	ErrCheckpoint = errors.New("OpenCode native checkpoint is invalid")
	// ErrUncertainEffect prevents automatic replay when native history contains an unfinished tool.
	// ErrUncertainEffect запрещает автоматический повтор при незавершённом инструменте в native history.
	ErrUncertainEffect = errors.New("OpenCode native tool outcome requires reconciliation")
)

// Checkpoint describes a validated official export; it does not authorize its owner or prove writer join.
// Checkpoint описывает проверенный официальный export, но не авторизует владельца и не доказывает writer join.
type Checkpoint struct {
	SessionID string
	SHA256    string
	SizeBytes int64
	Messages  int
}

// InspectCheckpoint verifies the qualified native format and exact identities, leaving transcript decoding
// and restoration to upstream import. The caller must first verify its server-owned digest and scope.
// InspectCheckpoint проверяет закреплённый native-формат и точные ID; разбор истории и restore выполняет
// upstream import. Вызывающий сначала проверяет серверные digest и область владельца.
func InspectCheckpoint(raw []byte, sessionID string) (Checkpoint, error) {
	if !nativeSessionPattern.MatchString(sessionID) || len(raw) == 0 || len(raw) > MaximumCheckpointBytes {
		return Checkpoint{}, ErrCheckpoint
	}
	var export struct {
		Info struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"info"`
		Messages []Message `json:"messages"`
	}
	if decodeNativeJSON(raw, &export) != nil || export.Info.ID != sessionID || export.Info.Version != runtimecontract.OpenCodeVersion || export.Messages == nil {
		return Checkpoint{}, ErrCheckpoint
	}
	seenMessages, seenParts := map[string]bool{}, map[string]bool{}
	for _, message := range export.Messages {
		var info struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionID"`
			Role      string `json:"role"`
		}
		if json.Unmarshal(message.Info, &info) != nil || !nativeMessagePattern.MatchString(info.ID) ||
			info.SessionID != sessionID || (info.Role != "user" && info.Role != "assistant") || seenMessages[info.ID] {
			return Checkpoint{}, ErrCheckpoint
		}
		seenMessages[info.ID] = true
		for _, rawPart := range message.Parts {
			var part struct {
				ID        string `json:"id"`
				SessionID string `json:"sessionID"`
				MessageID string `json:"messageID"`
				Type      string `json:"type"`
				State     struct {
					Status string `json:"status"`
				} `json:"state"`
			}
			if json.Unmarshal(rawPart, &part) != nil || !nativePartPattern.MatchString(part.ID) ||
				part.SessionID != sessionID || part.MessageID != info.ID || part.Type == "" || seenParts[part.ID] {
				return Checkpoint{}, ErrCheckpoint
			}
			seenParts[part.ID] = true
			if part.Type == "tool" && part.State.Status != "completed" && part.State.Status != "error" {
				return Checkpoint{}, ErrUncertainEffect
			}
		}
	}
	hash := sha256.Sum256(raw)
	return Checkpoint{SessionID: sessionID, SHA256: hex.EncodeToString(hash[:]), SizeBytes: int64(len(raw)), Messages: len(export.Messages)}, nil
}

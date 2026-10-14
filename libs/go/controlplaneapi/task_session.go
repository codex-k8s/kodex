package controlplaneapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

const TaskSessionMaximumProjectionBytes = 512 << 10
const TaskSessionMaximumMessages = 10

var errTaskSessionProjection = errors.New("task session projection invalid")
var taskSessionRef = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)
var taskSessionDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Курсор — locator страницы, никогда не authority. CP перечитывает весь scope.
type TaskSessionCursor struct {
	Version         int
	Binding, Source string
	Offset          int
}

func ReadTaskSessionCursor(token string) (TaskSessionCursor, error) {
	var cursor TaskSessionCursor
	if token == "" {
		return cursor, nil
	}
	if len(token) > 1024 {
		return cursor, errTaskSessionProjection
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != token {
		return cursor, errTaskSessionProjection
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cursor) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) || cursor.Version != 1 || !taskSessionDigest.MatchString(cursor.Binding) || !taskSessionDigest.MatchString(cursor.Source) || cursor.Offset < 1 || cursor.Offset > 10000 {
		return TaskSessionCursor{}, errTaskSessionProjection
	}
	canonical, _ := json.Marshal(cursor)
	if !bytes.Equal(raw, canonical) {
		return TaskSessionCursor{}, errTaskSessionProjection
	}
	return cursor, nil
}

// Формат v1 задаёт точные JSON bytes для digest: фиксированные поля, decimal
// strings для int64 и стандартный encoding/json, без protobuf canonical claims.
type TaskSessionPublishedMessage struct {
	EventRef         string `json:"event_ref"`
	MessageRef       string `json:"message_ref"`
	Phase            string `json:"phase"`
	Text             string `json:"text"`
	Origin           string `json:"origin"`
	SourceRunRef     string `json:"source_run_ref"`
	SourceRunVersion int64  `json:"source_run_version,string"`
	SessionRef       string `json:"session_ref"`
	NodeRef          string `json:"node_ref"`
	TurnRef          string `json:"turn_ref"`
	TurnNumber       int64  `json:"turn_number,string"`
	Attempt          int32  `json:"attempt"`
	EventSequence    int64  `json:"event_sequence,string"`
	MessageRevision  int64  `json:"message_revision,string"`
}
type TaskSessionProjection struct {
	Version             int                           `json:"version"`
	Coverage            string                        `json:"coverage"`
	Order               string                        `json:"order"`
	RunRef              string                        `json:"run_ref"`
	ProjectRef          string                        `json:"project_ref"`
	SessionRef          string                        `json:"session_ref"`
	Title               string                        `json:"title"`
	State               string                        `json:"state"`
	RunVersion          int64                         `json:"run_version,string"`
	ResultSummary       string                        `json:"result_summary"`
	SafeErrorCode       string                        `json:"safe_error_code"`
	SafeErrorMessage    string                        `json:"safe_error_message"`
	SessionStorageState string                        `json:"session_storage_state"`
	Messages            []TaskSessionPublishedMessage `json:"messages"`
	SourceSHA256        string                        `json:"source_sha256"`
	NextCursor          string                        `json:"next_cursor"`
	Truncated           bool                          `json:"truncated"`
	ProjectionSHA256    string                        `json:"projection_sha256,omitempty"`
}

func taskSessionProjection(page *controlplanev1.AssistantTaskSessionPage) (TaskSessionProjection, error) {
	var result TaskSessionProjection
	if page == nil || len(page.ProtoReflect().GetUnknown()) != 0 || !taskSessionRef.MatchString(page.RunRef) || !taskSessionRef.MatchString(page.SessionRef) || (page.ProjectRef != "" && !taskSessionRef.MatchString(page.ProjectRef)) || page.RunVersion < 1 || !taskSessionDigest.MatchString(page.SourceSha256) || len(page.Messages) > TaskSessionMaximumMessages || len(page.NextCursor) > 1024 || page.Truncated != (page.NextCursor != "") || (page.Truncated && len(page.Messages) == 0) {
		return result, errTaskSessionProjection
	}
	if page.NextCursor != "" {
		next, err := ReadTaskSessionCursor(page.NextCursor)
		if err != nil || next.Source != page.SourceSha256 {
			return result, errTaskSessionProjection
		}
	}
	state := strings.TrimPrefix(page.State.String(), "RUN_STATE_")
	switch state {
	case "QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING", "SUCCEEDED", "FAILED", "CANCELLED":
	default:
		return result, errTaskSessionProjection
	}
	switch page.SessionStorageState {
	case "UNTRACKED", "LIVE", "SNAPSHOT_READY", "SNAPSHOTTING", "DELETE_PVC_READY", "ARCHIVED", "RESTORE_READY", "RESTORING", "ERROR", "PURGED":
	default:
		return result, errTaskSessionProjection
	}
	for _, value := range []string{page.Title, page.ResultSummary, page.SafeErrorCode, page.SafeErrorMessage} {
		if !utf8.ValidString(value) {
			return result, errTaskSessionProjection
		}
	}
	if len([]rune(page.Title)) < 1 || len([]rune(page.Title)) > 300 || len(page.ResultSummary) > 16000 || len(page.SafeErrorCode) > 128 || len(page.SafeErrorMessage) > 8000 {
		return result, errTaskSessionProjection
	}
	result = TaskSessionProjection{Version: 1, Coverage: "SELECTED_SESSION_PUBLIC_MESSAGES", Order: "NEWEST_FIRST", RunRef: page.RunRef, ProjectRef: page.ProjectRef, SessionRef: page.SessionRef, Title: page.Title, State: state, RunVersion: page.RunVersion, ResultSummary: page.ResultSummary, SafeErrorCode: page.SafeErrorCode, SafeErrorMessage: page.SafeErrorMessage, SessionStorageState: page.SessionStorageState, SourceSHA256: page.SourceSha256, NextCursor: page.NextCursor, Truncated: page.Truncated, Messages: []TaskSessionPublishedMessage{}}
	seen := map[string]bool{}
	for _, message := range page.Messages {
		if message == nil || len(message.ProtoReflect().GetUnknown()) != 0 || !taskSessionRef.MatchString(message.EventRef) || seen[message.EventRef] || !taskSessionRef.MatchString(message.MessageRef) || !taskSessionRef.MatchString(message.SourceRunRef) || !taskSessionRef.MatchString(message.NodeRef) || !taskSessionRef.MatchString(message.TurnRef) || message.SessionRef != page.SessionRef || message.SourceRunVersion < 1 || message.TurnNumber < 1 || message.Attempt < 1 || message.EventSequence < 1 || message.MessageRevision != 1 || message.Text == "" || !utf8.ValidString(message.Text) {
			return TaskSessionProjection{}, errTaskSessionProjection
		}
		phase := strings.TrimPrefix(message.Phase.String(), "ASSISTANT_TASK_MESSAGE_PHASE_")
		switch phase {
		case "USER":
			if message.MessageRef != message.TurnRef {
				return TaskSessionProjection{}, errTaskSessionProjection
			}
		case "COMMENTARY", "FINAL":
		default:
			return TaskSessionProjection{}, errTaskSessionProjection
		}
		origin := strings.TrimPrefix(message.Origin.String(), "ASSISTANT_TASK_MESSAGE_ORIGIN_")
		if origin != "ORDINARY" && !(origin == "CALLBACK_CONTINUATION" && phase == "USER" && message.Text == "i18n:CALLBACK_CONTINUATION_PUBLIC") {
			return TaskSessionProjection{}, errTaskSessionProjection
		}
		seen[message.EventRef] = true
		result.Messages = append(result.Messages, TaskSessionPublishedMessage{message.EventRef, message.MessageRef, phase, message.Text, origin, message.SourceRunRef, message.SourceRunVersion, message.SessionRef, message.NodeRef, message.TurnRef, message.TurnNumber, message.Attempt, message.EventSequence, message.MessageRevision})
	}
	return result, nil
}

// Producer и consumer используют одну закрытую проекцию, а не raw ProtoJSON.
func SealTaskSessionPage(page *controlplanev1.AssistantTaskSessionPage) error {
	projection, err := taskSessionProjection(page)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(projection)
	if err != nil {
		return errTaskSessionProjection
	}
	digest := sha256.Sum256(raw)
	page.ProjectionSha256 = hex.EncodeToString(digest[:])
	projection.ProjectionSHA256 = page.ProjectionSha256
	raw, err = json.Marshal(projection)
	if err != nil || len(raw) > TaskSessionMaximumProjectionBytes {
		return errTaskSessionProjection
	}
	return nil
}
func ProjectTaskSessionPage(page *controlplanev1.AssistantTaskSessionPage) (TaskSessionProjection, error) {
	projection, err := taskSessionProjection(page)
	if err != nil {
		return TaskSessionProjection{}, err
	}
	raw, err := json.Marshal(projection)
	if err != nil {
		return TaskSessionProjection{}, errTaskSessionProjection
	}
	digest := sha256.Sum256(raw)
	if page.ProjectionSha256 != hex.EncodeToString(digest[:]) {
		return TaskSessionProjection{}, errTaskSessionProjection
	}
	projection.ProjectionSHA256 = page.ProjectionSha256
	raw, err = json.Marshal(projection)
	if err != nil || len(raw) > TaskSessionMaximumProjectionBytes {
		return TaskSessionProjection{}, errTaskSessionProjection
	}
	return projection, nil
}

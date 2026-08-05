package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	"github.com/google/uuid"
)

type RuntimeMCPBindingRequest struct {
	ControlSessionID string `json:"control_session_id"`
	ChannelID        string `json:"channel_id"`
	RootPostID       string `json:"root_post_id"`
	BotStableKey     string `json:"bot_stable_key"`
	ExecutionID      string `json:"execution_id"`
	TurnID           string `json:"turn_id"`
	Attempt          uint32 `json:"attempt"`
}

type RuntimeMCPBinding struct {
	AgentSessionKey           string `json:"agent_session_key"`
	AgentSessionID            int64  `json:"agent_session_id"`
	AgentSessionVersion       uint64 `json:"agent_session_version"`
	AgentSessionBindingSHA256 string `json:"agent_session_binding_sha256"`
	ImmutableSecretRef        string `json:"immutable_secret_ref"`
	ProviderContentVersion    string `json:"provider_content_version"`
	ContentSHA256             string `json:"content_sha256"`
	ExecutionID               string `json:"execution_id"`
	TurnID                    string `json:"turn_id"`
	Attempt                   uint32 `json:"attempt"`
}

type runtimeMCPState struct {
	Transport      string `json:"transport"`
	BindingVersion uint64 `json:"mcp_binding_version,omitempty"`
	ExecutionID    string `json:"execution_id,omitempty"`
	TurnID         string `json:"turn_id,omitempty"`
	Attempt        uint32 `json:"attempt,omitempty"`
}

// EnsureRuntimeMCPBinding создаёт только transport identity. Он не создаёт
// legacy Turn/Run/Pod и не принимает решения жизненного цикла control-plane.
func (svc *AgentSessionService) EnsureRuntimeMCPBinding(ctx context.Context,
	request RuntimeMCPBindingRequest) (RuntimeMCPBinding, error) {
	if !svc.cfg.StorageReady || !svc.cfg.RuntimeReady || svc.cfg.Store == nil || svc.cfg.RuntimeRunner == nil ||
		uuid.Validate(request.ControlSessionID) != nil || uuid.Validate(request.ExecutionID) != nil ||
		uuid.Validate(request.TurnID) != nil || request.Attempt == 0 || request.ChannelID == "" || request.RootPostID == "" ||
		request.BotStableKey == "" || strings.ContainsAny(request.ChannelID+request.RootPostID+request.BotStableKey, "\x00\r\n") {
		return RuntimeMCPBinding{}, errors.New("runtime MCP binding request is invalid")
	}
	chat, err := svc.cfg.Store.GetChatByMattermostChannelID(ctx, request.ChannelID)
	if err != nil {
		return RuntimeMCPBinding{}, err
	}
	identities, err := svc.cfg.Store.ListMattermostBotIdentitiesByProject(ctx, chat.ProjectID)
	if err != nil {
		return RuntimeMCPBinding{}, err
	}
	var roleID int64
	for _, identity := range identities {
		if identity.Status == "configured" && (strings.EqualFold(identity.Username, request.BotStableKey) ||
			identity.MattermostUserID == request.BotStableKey) {
			roleID = identity.RoleID
			break
		}
	}
	role, err := svc.cfg.Store.GetAgentRole(ctx, roleID)
	if err != nil || role.ProjectID != chat.ProjectID {
		return RuntimeMCPBinding{}, errors.New("runtime MCP role binding is unavailable")
	}
	sessionKey := "owner:" + request.ControlSessionID
	session, err := svc.cfg.Store.GetAgentSession(ctx, sessionKey)
	if errors.Is(err, adminrepo.ErrNotFound) {
		session, _, err = svc.cfg.Store.UpsertAgentSession(ctx, adminrepo.UpsertAgentSessionInput{
			SessionKey: sessionKey, ProjectID: chat.ProjectID, ChatID: chat.ID, RoleID: role.ID,
			SessionScope: "control-plane", MattermostChannelID: request.ChannelID,
			MattermostRootPostID: request.RootPostID, TTLSeconds: defaultThreadSessionTTLSeconds,
			Capabilities: `{"transport":"control-plane"}`,
		})
	}
	if err != nil || session.ProjectID != chat.ProjectID || session.ChatID != chat.ID || session.RoleID != role.ID ||
		session.MattermostChannelID != request.ChannelID || session.MattermostRootPostID != request.RootPostID {
		return RuntimeMCPBinding{}, errors.New("runtime MCP session mapping is unavailable")
	}
	preparer, ok := svc.cfg.RuntimeRunner.(runtimerepo.RuntimeMCPTokenPreparer)
	if !ok {
		return RuntimeMCPBinding{}, errors.New("runtime MCP token producer is unavailable")
	}
	secret, err := preparer.EnsureRuntimeMCPToken(ctx, runtimerepo.RuntimeMCPTokenInput{SessionKey: sessionKey,
		ExecutionID: request.ExecutionID, TurnID: request.TurnID, Attempt: request.Attempt})
	if err != nil || secret.Namespace != "mattercodex-system" || secret.SecretName == "" ||
		secret.Integrity.UID == "" || secret.Integrity.ResourceVersion == "" ||
		len(secret.Integrity.ContentSHA256) != sha256.Size*2 {
		return RuntimeMCPBinding{}, errors.New("runtime MCP token readback is unavailable")
	}
	// Каждый turn получает свежую server-owned revision AgentSession даже при
	// неизменном Secret. После rotation те же поля дополнительно связываются с
	// новым UID/resourceVersion/content digest; stale RuntimeRevision закрывается.
	currentState, err := parseRuntimeMCPState(session.Capabilities)
	if err != nil || (session.TokenSecretRef != "" && currentState.BindingVersion == 0) {
		return RuntimeMCPBinding{}, errors.New("runtime MCP binding state is invalid")
	}
	version := currentState.BindingVersion
	if session.TokenSecretRef == secret.SecretName {
		if currentState.ExecutionID != request.ExecutionID || currentState.TurnID != request.TurnID ||
			currentState.Attempt != request.Attempt || version == 0 {
			return RuntimeMCPBinding{}, errors.New("runtime MCP binding replay is stale")
		}
	} else {
		if version == ^uint64(0) {
			return RuntimeMCPBinding{}, errors.New("runtime MCP token revision is exhausted")
		}
		version++
		stateRaw, encodeErr := json.Marshal(runtimeMCPState{Transport: "control-plane", BindingVersion: version,
			ExecutionID: request.ExecutionID, TurnID: request.TurnID, Attempt: request.Attempt})
		if encodeErr != nil {
			return RuntimeMCPBinding{}, errors.New("encode runtime MCP binding state")
		}
		session, err = svc.cfg.Store.UpdateAgentSessionRuntime(ctx, adminrepo.UpdateAgentSessionRuntimeInput{
			SessionKey: sessionKey, Status: agentSessionStatusIdle, TokenSecretRef: secret.SecretName,
			Capabilities: string(stateRaw), ExpectedCapabilities: session.Capabilities,
			ExtendTTLSeconds: defaultThreadSessionTTLSeconds,
		})
	}
	if err != nil || session.TokenSecretRef != secret.SecretName {
		return RuntimeMCPBinding{}, errors.New("runtime MCP token binding is unavailable")
	}
	// Смена TokenSecretRef уже закрыла predecessor в consumer; удаление здесь
	// является best-effort cleanup, повторяемым на каждом следующем binding.
	_ = preparer.ReconcileRuntimeMCPTokens(ctx, sessionKey, secret.SecretName)
	persistedState, stateErr := parseRuntimeMCPState(session.Capabilities)
	if stateErr != nil || persistedState.BindingVersion != version || persistedState.ExecutionID != request.ExecutionID ||
		persistedState.TurnID != request.TurnID || persistedState.Attempt != request.Attempt || version == 0 {
		return RuntimeMCPBinding{}, errors.New("runtime MCP token revision is invalid")
	}
	providerVersion := secret.Integrity.UID + ":" + secret.Integrity.ResourceVersion
	digest := sha256.Sum256([]byte(strings.Join([]string{sessionKey, request.ControlSessionID,
		request.ChannelID, request.RootPostID, secret.SecretName, providerVersion,
		secret.Integrity.ContentSHA256, request.ExecutionID, request.TurnID,
		fmt.Sprintf("%d", request.Attempt), fmt.Sprintf("%d", version)}, "\x00")))
	return RuntimeMCPBinding{AgentSessionKey: sessionKey, AgentSessionID: session.ID,
		AgentSessionVersion: version, AgentSessionBindingSHA256: hex.EncodeToString(digest[:]),
		ImmutableSecretRef:     "k8s-immutable-secret://" + secret.Namespace + "/" + secret.SecretName,
		ProviderContentVersion: providerVersion, ContentSHA256: secret.Integrity.ContentSHA256,
		ExecutionID: request.ExecutionID, TurnID: request.TurnID, Attempt: request.Attempt}, nil
}

func parseRuntimeMCPState(raw string) (runtimeMCPState, error) {
	if strings.TrimSpace(raw) == "" {
		return runtimeMCPState{}, errors.New("runtime MCP capabilities are missing")
	}
	var state runtimeMCPState
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || decoder.Decode(&struct{}{}) != io.EOF || state.Transport != "control-plane" {
		return runtimeMCPState{}, errors.New("runtime MCP capabilities are invalid")
	}
	if state.BindingVersion == 0 {
		if state.ExecutionID != "" || state.TurnID != "" || state.Attempt != 0 {
			return runtimeMCPState{}, errors.New("runtime MCP capabilities are incomplete")
		}
		return state, nil
	}
	if uuid.Validate(state.ExecutionID) != nil || uuid.Validate(state.TurnID) != nil || state.Attempt == 0 {
		return runtimeMCPState{}, errors.New("runtime MCP execution capabilities are invalid")
	}
	return state, nil
}

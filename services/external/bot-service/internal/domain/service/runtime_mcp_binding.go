package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
}

type RuntimeMCPBinding struct {
	AgentSessionKey           string `json:"agent_session_key"`
	AgentSessionID            int64  `json:"agent_session_id"`
	AgentSessionVersion       uint64 `json:"agent_session_version"`
	AgentSessionBindingSHA256 string `json:"agent_session_binding_sha256"`
	ImmutableSecretRef        string `json:"immutable_secret_ref"`
	ProviderContentVersion    string `json:"provider_content_version"`
	ContentSHA256             string `json:"content_sha256"`
}

// EnsureRuntimeMCPBinding создаёт только transport identity. Он не создаёт
// legacy Turn/Run/Pod и не принимает решения жизненного цикла control-plane.
func (svc *AgentSessionService) EnsureRuntimeMCPBinding(ctx context.Context,
	request RuntimeMCPBindingRequest) (RuntimeMCPBinding, error) {
	if !svc.cfg.StorageReady || !svc.cfg.RuntimeReady || svc.cfg.Store == nil || svc.cfg.RuntimeRunner == nil ||
		uuid.Validate(request.ControlSessionID) != nil || request.ChannelID == "" || request.RootPostID == "" ||
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
	secret, err := preparer.EnsureRuntimeMCPToken(ctx, sessionKey)
	if err != nil || secret.Namespace != "mattercodex-system" || secret.SecretName == "" ||
		secret.Integrity.UID == "" || secret.Integrity.ResourceVersion == "" ||
		len(secret.Integrity.ContentSHA256) != sha256.Size*2 {
		return RuntimeMCPBinding{}, errors.New("runtime MCP token readback is unavailable")
	}
	// Каждый turn получает свежую server-owned revision AgentSession даже при
	// неизменном Secret. После rotation те же поля дополнительно связываются с
	// новым UID/resourceVersion/content digest; stale RuntimeRevision закрывается.
	session, err = svc.cfg.Store.UpdateAgentSessionRuntime(ctx, adminrepo.UpdateAgentSessionRuntimeInput{
		SessionKey: sessionKey, Status: agentSessionStatusIdle, TokenSecretRef: secret.SecretName,
		ExtendTTLSeconds: defaultThreadSessionTTLSeconds,
	})
	if err != nil || session.TokenSecretRef != secret.SecretName {
		return RuntimeMCPBinding{}, errors.New("runtime MCP token binding is unavailable")
	}
	version := uint64(session.UpdatedAt.UTC().UnixMicro())
	if version == 0 {
		return RuntimeMCPBinding{}, errors.New("runtime MCP token revision is invalid")
	}
	providerVersion := secret.Integrity.UID + ":" + secret.Integrity.ResourceVersion
	digest := sha256.Sum256([]byte(strings.Join([]string{sessionKey, request.ControlSessionID,
		request.ChannelID, request.RootPostID, secret.SecretName, providerVersion,
		secret.Integrity.ContentSHA256}, "\x00")))
	return RuntimeMCPBinding{AgentSessionKey: sessionKey, AgentSessionID: session.ID,
		AgentSessionVersion: version, AgentSessionBindingSHA256: hex.EncodeToString(digest[:]),
		ImmutableSecretRef:     "k8s-immutable-secret://" + secret.Namespace + "/" + secret.SecretName,
		ProviderContentVersion: providerVersion, ContentSHA256: secret.Integrity.ContentSHA256}, nil
}

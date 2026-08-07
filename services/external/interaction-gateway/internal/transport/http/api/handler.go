// Package api реализует generated OpenAPI adapter Mattermost boundary.
package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"

	domaincontrol "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/controlplane"
	domainmattermost "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/mattermost"
	domainerrs "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/errs"
	domainservice "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/service/gateway"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/security/readbackgrant"
	generated "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

type Config struct {
	SlashToken                  string
	MaximumBodyBytes            int64
	MaximumRuntimeOutputBytes   int64
	MattermostClientSPIFFE      string
	ReadbackClientSPIFFEIDs     []string
	MaterializationClientSPIFFE string
}

type Handler struct {
	service        *domainservice.Service
	config         Config
	readbackSPIFFE map[string]struct{}
	readbackGrant  *readbackgrant.Verifier
}

func New(service *domainservice.Service, readbackGrant *readbackgrant.Verifier, config Config) (*Handler, error) {
	if service == nil || len(config.SlashToken) < 16 || config.MaximumBodyBytes < 1024 ||
		config.MaximumBodyBytes > 1<<20 || config.MaximumRuntimeOutputBytes < 1<<20 ||
		config.MaximumRuntimeOutputBytes > 256<<20 || !strings.HasPrefix(config.MattermostClientSPIFFE, "spiffe://") ||
		len(config.ReadbackClientSPIFFEIDs) == 0 || len(config.ReadbackClientSPIFFEIDs) > 8 || readbackGrant == nil {
		return nil, errors.New("interaction HTTP handler configuration is invalid")
	}
	readback := make(map[string]struct{}, len(config.ReadbackClientSPIFFEIDs))
	for _, identity := range config.ReadbackClientSPIFFEIDs {
		if !strings.HasPrefix(identity, "spiffe://") {
			return nil, errors.New("interaction readback identity is invalid")
		}
		readback[identity] = struct{}{}
	}
	if !strings.HasPrefix(config.MaterializationClientSPIFFE, "spiffe://") {
		return nil, errors.New("runtime materialization identity is invalid")
	}
	return &Handler{service: service, config: config, readbackSPIFFE: readback, readbackGrant: readbackGrant}, nil
}

func (handler *Handler) GetRuntimeMaterialization(response http.ResponseWriter, request *http.Request,
	executionID openapi_types.UUID, artifactID openapi_types.UUID, params generated.GetRuntimeMaterializationParams,
) {
	identity, ok := peerSPIFFE(request)
	if !ok || identity != handler.config.MaterializationClientSPIFFE {
		writeError(response, http.StatusForbidden, "runtime materialization identity is not allowed")
		return
	}
	authorization := request.Header.Get("Authorization")
	grant := strings.TrimPrefix(authorization, "Bearer ")
	if grant == "" || len(grant) > 16<<10 || strings.TrimSpace(grant) != grant || authorization != "Bearer "+grant ||
		params.Version < 1 || len(params.Sha256) != 64 {
		writeError(response, http.StatusUnauthorized, "runtime materialization credential is not allowed")
		return
	}
	raw, _, err := handler.service.ReadRuntimeMaterialization(request.Context(), grant,
		executionID.String(), artifactID.String(), uint64(params.Version), params.Sha256)
	if err != nil {
		writeError(response, http.StatusServiceUnavailable, "runtime materialization is unavailable")
		return
	}
	response.Header().Set("Content-Type", "application/octet-stream")
	response.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	response.Header().Set("X-MatterCodex-Artifact-Version", strconv.FormatInt(params.Version, 10))
	response.Header().Set("X-MatterCodex-Artifact-SHA256", params.Sha256)
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(raw)
}

func (handler *Handler) PutRuntimeOutput(response http.ResponseWriter, request *http.Request,
	executionID openapi_types.UUID, params generated.PutRuntimeOutputParams,
) {
	identity, ok := peerSPIFFE(request)
	if !ok || identity != handler.config.MaterializationClientSPIFFE {
		writeError(response, http.StatusForbidden, "runtime output identity is not allowed")
		return
	}
	authorization := request.Header.Get("Authorization")
	grant := strings.TrimPrefix(authorization, "Bearer ")
	if grant == "" || len(grant) > 16<<10 || strings.TrimSpace(grant) != grant || authorization != "Bearer "+grant ||
		request.Header.Get("Content-Type") != "application/octet-stream" || request.ContentLength < 1 ||
		request.ContentLength > handler.config.MaximumRuntimeOutputBytes || !params.XMatterCodexOutputKind.Valid() ||
		params.XMatterCodexOutputSequence < 1 || params.XMatterCodexOutputTotal < 1 ||
		params.XMatterCodexOutputSequence > params.XMatterCodexOutputTotal || params.XMatterCodexOutputTotal > 4096 ||
		len(params.XMatterCodexOutputSHA256) != 64 {
		writeError(response, http.StatusUnauthorized, "runtime output credential is not allowed")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, handler.config.MaximumRuntimeOutputBytes)
	staged, err := os.CreateTemp("", "mattercodex-runtime-output-*")
	if err != nil || staged.Chmod(0o600) != nil {
		if staged != nil {
			_ = staged.Close()
			_ = os.Remove(staged.Name())
		}
		writeError(response, http.StatusServiceUnavailable, "runtime output staging is unavailable")
		return
	}
	defer func() {
		_ = staged.Close()
		_ = os.Remove(staged.Name())
	}()
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(staged, hasher), request.Body)
	if err != nil || written != request.ContentLength || written > handler.config.MaximumRuntimeOutputBytes ||
		hex.EncodeToString(hasher.Sum(nil)) != params.XMatterCodexOutputSHA256 || staged.Sync() != nil {
		writeError(response, http.StatusBadRequest, "runtime output content is invalid")
		return
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		writeError(response, http.StatusServiceUnavailable, "runtime output staging is unavailable")
		return
	}
	artifact, err := handler.service.StageRuntimeOutput(request.Context(), grant, executionID.String(),
		domaincontrol.RuntimeOutputMetadata{
			Kind: string(params.XMatterCodexOutputKind),
			Name: params.XMatterCodexOutputName, MediaType: params.XMatterCodexOutputMediaType,
			SizeBytes: uint64(written), SHA256: params.XMatterCodexOutputSHA256,
			Sequence: uint32(params.XMatterCodexOutputSequence), Total: uint32(params.XMatterCodexOutputTotal),
		}, staged)
	identifier, parseErr := uuid.Parse(artifact.ID)
	if err != nil || parseErr != nil || artifact.Version > math.MaxInt || artifact.SizeBytes > math.MaxInt64 {
		writeError(response, http.StatusServiceUnavailable, "runtime output staging is unavailable")
		return
	}
	writeJSON(response, http.StatusCreated, generated.RuntimeOutputReference{
		ArtifactId:      identifier,
		ArtifactVersion: int(artifact.Version), Sha256: artifact.SHA256, SizeBytes: int64(artifact.SizeBytes),
		Name: artifact.Name, MediaType: artifact.MediaType, StorageRef: artifact.StorageRef,
	})
}

func (handler *Handler) AcceptMattermostSlashCommand(response http.ResponseWriter, request *http.Request) {
	if !handler.requireMattermost(response, request) {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, handler.config.MaximumBodyBytes)
	if err := request.ParseForm(); err != nil {
		writeBodyError(response, err, "invalid Mattermost slash command")
		return
	}
	if request.URL.RawQuery != "" || !validSlashForm(request.Form) ||
		subtle.ConstantTimeCompare([]byte(request.Form.Get("token")), []byte(handler.config.SlashToken)) != 1 ||
		request.Form.Get("command") != "/codex" || request.Form.Get("trigger_id") == "" {
		writeError(response, http.StatusUnauthorized, "invalid Mattermost slash command")
		return
	}
	raw := domainmattermost.RawEvent{
		Kind: "SLASH", Revision: 1, TeamID: request.Form.Get("team_id"),
		ChannelID: request.Form.Get("channel_id"), UserID: request.Form.Get("user_id"),
		Text: request.Form.Get("text"),
	}
	raw.ProviderEventID = providerID("slash", request.Form.Get("trigger_id"), raw.TeamID, raw.ChannelID, raw.UserID)
	result, err := handler.service.HandleRaw(request.Context(), raw)
	writeResult(response, result, err)
}

func (handler *Handler) AcceptMattermostAction(response http.ResponseWriter, request *http.Request) {
	if !handler.requireMattermost(response, request) {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, handler.config.MaximumBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var callback generated.ActionCallback
	if err := decoder.Decode(&callback); err != nil {
		writeBodyError(response, err, "invalid Mattermost action callback")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF || !callback.Context.Action.Valid() {
		writeError(response, http.StatusBadRequest, "invalid Mattermost action callback")
		return
	}
	raw := domainmattermost.RawEvent{
		Kind: "ACTION", Revision: 1, TeamID: callback.TeamId, ChannelID: callback.ChannelId,
		PostID: callback.PostId, UserID: callback.UserId,
		ProviderEventID: providerID("action", callback.Context.DeliveryId.String(), callback.UserId, string(callback.Context.Action)),
	}
	if callback.Context.Action == generated.OPENDIALOG {
		if callback.TriggerId == nil {
			writeError(response, http.StatusBadRequest, "Mattermost dialog trigger is missing")
			return
		}
		result, err := handler.service.OpenDecisionDialog(request.Context(), raw,
			callback.Context.DeliveryId.String(), callback.Context.CallbackToken, *callback.TriggerId)
		writeResult(response, result, err)
		return
	}
	if callback.Context.Action == generated.STOP || callback.Context.Action == generated.RETRY {
		result, err := handler.service.HandleRuntimeAction(request.Context(), raw,
			callback.Context.DeliveryId.String(), callback.Context.CallbackToken,
			string(callback.Context.Action))
		writeResult(response, result, err)
		return
	}
	decision, ok := parseDecision(string(callback.Context.Action))
	if !ok {
		writeError(response, http.StatusBadRequest, "invalid owner decision")
		return
	}
	reason := "Mattermost action"
	if callback.Context.Value != nil && strings.TrimSpace(*callback.Context.Value) != "" {
		reason = strings.TrimSpace(*callback.Context.Value)
	}
	result, err := handler.service.HandleDecision(request.Context(), raw,
		callback.Context.DeliveryId.String(), callback.Context.CallbackToken, decision, reason)
	writeResult(response, result, err)
}

func (handler *Handler) AcceptMattermostDialog(response http.ResponseWriter, request *http.Request) {
	if !handler.requireMattermost(response, request) {
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, handler.config.MaximumBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var submission struct {
		Type       string            `json:"type"`
		CallbackID string            `json:"callback_id"`
		State      string            `json:"state"`
		UserID     string            `json:"user_id"`
		ChannelID  string            `json:"channel_id"`
		TeamID     string            `json:"team_id"`
		Submission map[string]string `json:"submission"`
		Cancelled  bool              `json:"cancelled"`
	}
	if err := decoder.Decode(&submission); err != nil {
		writeBodyError(response, err, "invalid Mattermost dialog submission")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF || submission.Type != "dialog_submission" ||
		submission.CallbackID != "owner-decision" || submission.Cancelled {
		writeError(response, http.StatusBadRequest, "invalid Mattermost dialog submission")
		return
	}
	stateRaw, err := base64.RawURLEncoding.DecodeString(submission.State)
	if err != nil || len(stateRaw) > 16<<10 {
		writeError(response, http.StatusBadRequest, "invalid Mattermost dialog state")
		return
	}
	var state struct {
		DeliveryID    string `json:"delivery_id"`
		CallbackToken string `json:"callback_token"`
		PostID        string `json:"post_id"`
	}
	stateDecoder := json.NewDecoder(strings.NewReader(string(stateRaw)))
	stateDecoder.DisallowUnknownFields()
	decision, ok := parseDecision(submission.Submission["decision"])
	if stateDecoder.Decode(&state) != nil || stateDecoder.Decode(&struct{}{}) != io.EOF ||
		uuid.Validate(state.DeliveryID) != nil || !ok ||
		strings.TrimSpace(submission.Submission["reason"]) == "" {
		writeError(response, http.StatusBadRequest, "invalid Mattermost dialog decision")
		return
	}
	raw := domainmattermost.RawEvent{
		Kind: "DIALOG", Revision: 1, TeamID: submission.TeamID, ChannelID: submission.ChannelID,
		PostID: state.PostID, UserID: submission.UserID,
		ProviderEventID: providerID("dialog", state.DeliveryID, submission.UserID, string(decision), submission.Submission["reason"]),
	}
	result, err := handler.service.HandleDecision(request.Context(), raw, state.DeliveryID,
		state.CallbackToken, decision, strings.TrimSpace(submission.Submission["reason"]))
	writeResult(response, result, err)
}

func (handler *Handler) GetInteractionDelivery(response http.ResponseWriter, request *http.Request, deliveryID openapi_types.UUID) {
	identity, ok := peerSPIFFE(request)
	if !ok {
		writeError(response, http.StatusUnauthorized, "workload identity is invalid")
		return
	}
	if _, allowed := handler.readbackSPIFFE[identity]; !allowed {
		writeError(response, http.StatusForbidden, "workload identity is not allowed")
		return
	}
	claims, err := handler.readbackGrant.Verify(request.Context(), request.Header.Get("Authorization"))
	if err != nil || claims.Readiness || claims.CallerSPIFFEID != identity || claims.DeliveryID != deliveryID.String() {
		writeError(response, http.StatusForbidden, "delivery readback credential is not allowed")
		return
	}
	if err := handler.service.ValidateDeliveryReadback(request.Context(), claims.JTI, claims.DeliveryID,
		claims.OrganizationID, claims.ProjectID, claims.CredentialSHA256, claims.Generation); err != nil {
		if errors.Is(err, domainerrs.ErrUnauthorized) {
			writeError(response, http.StatusForbidden, "delivery readback credential is not allowed")
		} else {
			writeError(response, http.StatusServiceUnavailable, "delivery readback authorization is unavailable")
		}
		return
	}
	delivery, err := handler.service.GetDeliveryScoped(request.Context(), claims.OrganizationID, claims.ProjectID, deliveryID.String())
	if err != nil {
		if errors.Is(err, domainerrs.ErrNotFound) {
			writeError(response, http.StatusNotFound, "interaction delivery not found")
		} else {
			writeError(response, http.StatusInternalServerError, "interaction delivery readback failed")
		}
		return
	}
	var payload map[string]any
	if json.Unmarshal(delivery.Payload, &payload) != nil {
		writeError(response, http.StatusInternalServerError, "delivery payload is unavailable")
		return
	}
	view := generated.DeliveryReadback{
		DeliveryId: deliveryID, Kind: generated.DeliveryReadbackKind(delivery.Kind),
		State: generated.DeliveryReadbackState(delivery.State), OrganizationId: uuid.MustParse(delivery.OrganizationID),
		ProjectId: uuid.MustParse(delivery.ProjectID), ChannelId: delivery.ChannelID,
		Payload: payload, PayloadSha256: delivery.PayloadSHA256, Attempts: int(delivery.Attempts),
		AckAttempts:    int(delivery.AckAttempts),
		Attachments:    make([]generated.ArtifactBindingReadback, 0, len(delivery.Attachments)),
		UploadReceipts: make([]generated.UploadReceiptReadback, 0, len(delivery.UploadReceipts)),
		CreatedAt:      delivery.CreatedAt, UpdatedAt: delivery.UpdatedAt,
	}
	for _, binding := range delivery.Attachments {
		artifactID, parseErr := uuid.Parse(binding.ArtifactID)
		if parseErr != nil || binding.SizeBytes > math.MaxInt64 || binding.Version > math.MaxInt {
			writeError(response, http.StatusInternalServerError, "delivery attachment readback is invalid")
			return
		}
		attachment := generated.ArtifactBindingReadback{
			ArtifactId: artifactID,
			Name:       binding.Name, Path: binding.Path, StorageRef: binding.StorageRef,
			SizeBytes: int64(binding.SizeBytes), MediaType: binding.MediaType, Sha256: binding.SHA256,
			Provenance: binding.Provenance, ScanState: generated.CLEAN,
		}
		if binding.Version > 0 {
			version := int(binding.Version)
			attachment.Version = &version
		}
		view.Attachments = append(view.Attachments, attachment)
	}
	for _, receipt := range delivery.UploadReceipts {
		artifactID, parseErr := uuid.Parse(receipt.ArtifactID)
		if parseErr != nil || receipt.SizeBytes > math.MaxInt64 {
			writeError(response, http.StatusInternalServerError, "delivery upload receipt readback is invalid")
			return
		}
		view.UploadReceipts = append(view.UploadReceipts, generated.UploadReceiptReadback{
			ArtifactId: artifactID, ProviderFileId: receipt.ProviderFileID,
			ChannelId: receipt.ChannelID, Name: receipt.Name, SizeBytes: int64(receipt.SizeBytes),
			MediaType: receipt.MediaType, Sha256: receipt.SHA256, CreatedAt: receipt.CreatedAt,
		})
	}
	if delivery.SessionID != "" {
		value := uuid.MustParse(delivery.SessionID)
		view.SessionId = &value
	}
	if delivery.TurnID != "" {
		value := uuid.MustParse(delivery.TurnID)
		view.TurnId = &value
	}
	if delivery.Attempt > 0 {
		value := int(delivery.Attempt)
		view.Attempt = &value
	}
	if delivery.ImmutableInputSHA256 != "" {
		view.InputSha256 = &delivery.ImmutableInputSHA256
	}
	if delivery.RootPostID != "" {
		view.RootPostId = &delivery.RootPostID
	}
	if delivery.ProviderPostID != "" {
		view.ProviderPostId = &delivery.ProviderPostID
		view.ProviderReceiptSha256 = &delivery.ProviderReceiptSHA256
	}
	if delivery.LastErrorCode != "" {
		view.LastErrorCode = &delivery.LastErrorCode
	}
	writeJSON(response, http.StatusOK, view)
}

func (handler *Handler) DownloadInteractionArtifact(response http.ResponseWriter, request *http.Request,
	grantID openapi_types.UUID,
) {
	binding, raw, err := handler.service.DownloadArtifact(request.Context(), grantID.String(), request.Header.Get("Authorization"))
	if err != nil {
		switch {
		case errors.Is(err, domainerrs.ErrUnauthorized):
			writeError(response, http.StatusUnauthorized, "Mattermost artifact credential is invalid")
		case errors.Is(err, domainerrs.ErrConflict):
			writeError(response, http.StatusConflict, "artifact download grant was already consumed")
		case errors.Is(err, domainerrs.ErrUnavailable):
			writeError(response, http.StatusServiceUnavailable, "artifact content is temporarily unavailable")
		default:
			writeError(response, http.StatusNotFound, "artifact download grant not found")
		}
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": binding.Name})
	if disposition == "" {
		writeError(response, http.StatusInternalServerError, "artifact filename is invalid")
		return
	}
	response.Header().Set("Content-Type", binding.MediaType)
	response.Header().Set("Content-Disposition", disposition)
	response.Header().Set("X-Content-SHA256", binding.SHA256)
	response.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(raw)
}

func (handler *Handler) requireMattermost(response http.ResponseWriter, request *http.Request) bool {
	identity, ok := peerSPIFFE(request)
	if !ok || identity != handler.config.MattermostClientSPIFFE {
		writeError(response, http.StatusUnauthorized, "Mattermost transport identity is invalid")
		return false
	}
	return true
}

func peerSPIFFE(request *http.Request) (string, bool) {
	if request.TLS == nil || len(request.TLS.VerifiedChains) != 1 || len(request.TLS.PeerCertificates) != 1 ||
		len(request.TLS.PeerCertificates[0].URIs) != 1 {
		return "", false
	}
	return request.TLS.PeerCertificates[0].URIs[0].String(), true
}

func parseDecision(value string) (enum.OwnerDecision, bool) {
	decision := enum.OwnerDecision(value)
	return decision, decision.Valid()
}

func writeResult(response http.ResponseWriter, result domainservice.Result, err error) {
	if err == nil || errors.Is(err, domainerrs.ErrIgnored) {
		text := result.Message
		if text == "" {
			text = "Ignored."
		}
		writeJSON(response, http.StatusOK, generated.MattermostResponse{ResponseType: generated.Ephemeral, Text: text})
		return
	}
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domainerrs.ErrUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, domainerrs.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domainerrs.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, domainerrs.ErrBusy), errors.Is(err, domainerrs.ErrUnavailable):
		status = http.StatusServiceUnavailable
	}
	message := result.Message
	var semantic interface{ ResponseMessage() string }
	if message == "" && errors.As(err, &semantic) {
		message = semantic.ResponseMessage()
	}
	if message == "" {
		message = err.Error()
	}
	writeError(response, status, message)
}

func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, generated.Error{Error: message})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeBodyError(response http.ResponseWriter, err error, message string) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(response, http.StatusRequestEntityTooLarge, "request body is too large")
		return
	}
	writeError(response, http.StatusBadRequest, message)
}

func validSlashForm(form map[string][]string) bool {
	allowed := map[string]struct{}{
		"token": {}, "team_id": {}, "team_domain": {}, "channel_id": {}, "channel_name": {},
		"user_id": {}, "user_name": {}, "command": {}, "text": {}, "response_url": {}, "trigger_id": {},
	}
	for key, values := range form {
		if _, ok := allowed[key]; !ok || len(values) != 1 {
			return false
		}
	}
	single := func(key string) string {
		if len(form[key]) != 1 {
			return ""
		}
		return form[key][0]
	}
	return single("team_id") != "" && single("channel_id") != "" && single("user_id") != "" &&
		single("text") != "" && len(single("text")) <= 16384
}

func providerID(kind string, fields ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return kind + ":" + hex.EncodeToString(digest[:])
}

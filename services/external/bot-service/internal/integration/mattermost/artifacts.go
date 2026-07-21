package mattermost

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	domainartifact "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/artifact"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

type artifactTokenResolver interface {
	GetMattermostBotTokenSecret(ctx context.Context, secretName string) (runtimerepo.MattermostBotTokenSecret, error)
}

type ArtifactDelivery struct {
	surface  *ControlSurface
	resolver artifactTokenResolver
}

var _ domainartifact.IncomingSource = (*ControlSurface)(nil)
var _ domainartifact.MattermostDelivery = (*ArtifactDelivery)(nil)

func NewArtifactDelivery(surface *ControlSurface, resolver artifactTokenResolver) (*ArtifactDelivery, error) {
	if surface == nil || resolver == nil {
		return nil, fmt.Errorf("mattermost artifact delivery requires surface and token resolver")
	}
	return &ArtifactDelivery{surface: surface, resolver: resolver}, nil
}

func (surface *ControlSurface) Metadata(ctx context.Context, fileID string) (domainartifact.SourceFile, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return domainartifact.SourceFile{}, fmt.Errorf("mattermost file identity is required")
	}
	info, _, err := surface.client.GetFileInfo(ctx, fileID)
	if err != nil {
		return domainartifact.SourceFile{}, fmt.Errorf("get Mattermost file metadata: %w", err)
	}
	if info == nil || info.Id != fileID || info.DeleteAt != 0 || info.Archived {
		return domainartifact.SourceFile{}, domainartifact.ErrScopeDenied
	}
	return domainartifact.SourceFile{
		FileID: info.Id, PostID: info.PostId, ChannelID: info.ChannelId, CreatorID: info.CreatorId,
		OriginalName: info.Name, DeclaredMediaType: info.MimeType, DeclaredSize: info.Size,
	}, nil
}

func (surface *ControlSurface) Open(ctx context.Context, fileID string) (io.ReadCloser, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return nil, fmt.Errorf("mattermost file identity is required")
	}
	endpoint := strings.TrimRight(surface.client.APIURL, "/") + "/files/" + url.PathEscape(fileID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build Mattermost file request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+surface.client.AuthToken)
	response, err := surface.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download Mattermost file: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, fmt.Errorf("download Mattermost file: unexpected status %d", response.StatusCode)
	}
	return response.Body, nil
}

func (delivery *ArtifactDelivery) Upload(ctx context.Context, request domainartifact.DeliveryRequest, body io.Reader) (string, error) {
	if strings.TrimSpace(request.DeliveryID) == "" || strings.TrimSpace(request.VersionID) == "" ||
		strings.TrimSpace(request.ChannelID) == "" || strings.TrimSpace(request.RootPostID) == "" ||
		strings.TrimSpace(request.BotTokenSecretRef) == "" || body == nil || request.Size < 0 || request.Size > domainartifact.DefaultMaxObjectBytes {
		return "", domainartifact.ErrScopeDenied
	}
	secret, err := delivery.resolver.GetMattermostBotTokenSecret(ctx, request.BotTokenSecretRef)
	if err != nil || strings.TrimSpace(secret.Token) == "" {
		return "", fmt.Errorf("resolve Mattermost artifact identity")
	}
	client := delivery.surface.clientWithToken(secret.Token)
	content, err := io.ReadAll(io.LimitReader(body, request.Size+1))
	if err != nil {
		return "", fmt.Errorf("read artifact delivery stream: %w", err)
	}
	if int64(len(content)) != request.Size {
		return "", fmt.Errorf("artifact delivery size mismatch")
	}
	digest := sha256.Sum256(content)
	if hex.EncodeToString(digest[:]) != request.SHA256 {
		return "", fmt.Errorf("artifact delivery checksum mismatch")
	}
	upload, _, err := client.UploadFile(ctx, content, request.ChannelID, request.FileName)
	for index := range content {
		content[index] = 0
	}
	if err != nil {
		return "", fmt.Errorf("upload Mattermost artifact: %w", err)
	}
	if upload == nil || len(upload.FileInfos) != 1 || upload.FileInfos[0] == nil || strings.TrimSpace(upload.FileInfos[0].Id) == "" {
		return "", fmt.Errorf("upload Mattermost artifact: response identity is invalid")
	}
	return strings.TrimSpace(upload.FileInfos[0].Id), nil
}

func (delivery *ArtifactDelivery) Publish(ctx context.Context, request domainartifact.DeliveryRequest, fileID string) (domainartifact.DeliveryReceipt, error) {
	fileID = strings.TrimSpace(fileID)
	if strings.TrimSpace(request.DeliveryID) == "" || strings.TrimSpace(request.VersionID) == "" ||
		strings.TrimSpace(request.ChannelID) == "" || strings.TrimSpace(request.RootPostID) == "" ||
		strings.TrimSpace(request.BotTokenSecretRef) == "" || fileID == "" {
		return domainartifact.DeliveryReceipt{}, domainartifact.ErrScopeDenied
	}
	secret, err := delivery.resolver.GetMattermostBotTokenSecret(ctx, request.BotTokenSecretRef)
	if err != nil || strings.TrimSpace(secret.Token) == "" {
		return domainartifact.DeliveryReceipt{}, fmt.Errorf("resolve Mattermost artifact identity")
	}
	client := delivery.surface.clientWithToken(secret.Token)
	if receipt, found, err := findArtifactDeliveryPost(ctx, client, request, fileID); err != nil || found {
		return receipt, err
	}
	post := &mattermostmodel.Post{
		ChannelId: request.ChannelID, RootId: request.RootPostID, Message: "#notrigger",
		FileIds: []string{fileID}, PendingPostId: request.DeliveryID,
		Props: artifactDeliveryProps(request),
	}
	created, _, createErr := client.CreatePost(ctx, post)
	if createErr == nil {
		if err := verifyArtifactDeliveryPost(created, request, fileID); err != nil {
			return domainartifact.DeliveryReceipt{}, err
		}
		return domainartifact.DeliveryReceipt{MattermostFileID: fileID, MattermostPostID: created.Id}, nil
	}
	receipt, found, reconcileErr := findArtifactDeliveryPost(ctx, client, request, fileID)
	if reconcileErr != nil {
		return domainartifact.DeliveryReceipt{}, errors.Join(createErr, reconcileErr)
	}
	if found {
		return receipt, nil
	}
	return domainartifact.DeliveryReceipt{}, createErr
}

func artifactDeliveryProps(request domainartifact.DeliveryRequest) map[string]any {
	return map[string]any{
		"matter_codex_event":                "artifact_delivery",
		"matter_codex_artifact_delivery_id": request.DeliveryID,
		"matter_codex_artifact_version_id":  request.VersionID,
		"matter_codex_notrigger":            true,
	}
}

func findArtifactDeliveryPost(ctx context.Context, client *mattermostmodel.Client4, request domainartifact.DeliveryRequest, expectedFileID string) (domainartifact.DeliveryReceipt, bool, error) {
	posts, _, err := client.GetPostThread(ctx, request.RootPostID, "", false)
	if err != nil {
		return domainartifact.DeliveryReceipt{}, false, fmt.Errorf("reconcile Mattermost artifact post: %w", err)
	}
	if posts == nil {
		return domainartifact.DeliveryReceipt{}, false, nil
	}
	var match *mattermostmodel.Post
	for _, post := range posts.Posts {
		if post == nil || artifactStringProp(post.Props, "matter_codex_artifact_delivery_id") != request.DeliveryID {
			continue
		}
		if match != nil {
			return domainartifact.DeliveryReceipt{}, false, domainartifact.ErrDeliveryAmbiguous
		}
		match = post
	}
	if match == nil {
		return domainartifact.DeliveryReceipt{}, false, nil
	}
	if len(match.FileIds) != 1 {
		return domainartifact.DeliveryReceipt{}, false, domainartifact.ErrDeliveryAmbiguous
	}
	if err := verifyArtifactDeliveryPost(match, request, expectedFileID); err != nil {
		return domainartifact.DeliveryReceipt{}, false, err
	}
	return domainartifact.DeliveryReceipt{MattermostFileID: match.FileIds[0], MattermostPostID: match.Id}, true, nil
}

func verifyArtifactDeliveryPost(post *mattermostmodel.Post, request domainartifact.DeliveryRequest, fileID string) error {
	if post == nil || strings.TrimSpace(post.Id) == "" || post.ChannelId != request.ChannelID || post.RootId != request.RootPostID ||
		post.Message != "#notrigger" || len(post.FileIds) != 1 || post.FileIds[0] != fileID {
		return domainartifact.ErrDeliveryAmbiguous
	}
	expected := artifactDeliveryProps(request)
	for key, value := range expected {
		actual, ok := post.Props[key]
		if !ok || !bytes.Equal([]byte(fmt.Sprint(actual)), []byte(fmt.Sprint(value))) {
			return domainartifact.ErrDeliveryAmbiguous
		}
	}
	for key := range post.Props {
		if strings.HasPrefix(key, "matter_codex_") {
			if _, ok := expected[key]; !ok {
				return domainartifact.ErrDeliveryAmbiguous
			}
		}
	}
	return nil
}

func artifactStringProp(props map[string]any, key string) string {
	value, _ := props[key].(string)
	return strings.TrimSpace(value)
}

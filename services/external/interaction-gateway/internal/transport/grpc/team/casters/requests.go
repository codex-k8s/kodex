// Package casters преобразует generated interactiongateway DTO в domain-safe значения и обратно.
package casters

import (
	"errors"
	"strings"

	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	"github.com/google/uuid"
)

func ListRequest(request *interactiongatewayv1.ListMattermostTeamsRequest) (uint32, string, error) {
	if request == nil || request.GetPageSize() > 100 ||
		(request.GetCursor() != "" && !validUUID(request.GetCursor())) {
		return 0, "", errors.New("mattermost team catalog request is invalid")
	}
	return request.GetPageSize(), request.GetCursor(), nil
}

func CreateRequest(request *interactiongatewayv1.CreateMattermostTeamRequest) (string, string, string, error) {
	if request == nil || len(request.GetDisplayName()) == 0 || len(request.GetDisplayName()) > 256 ||
		len(request.GetSlugIntent()) > 256 || strings.ContainsAny(request.GetDisplayName()+request.GetSlugIntent(), "\x00\r\n") ||
		!validUUID(request.GetIdempotencyKey()) {
		return "", "", "", errors.New("mattermost team create request is invalid")
	}
	return request.GetDisplayName(), request.GetSlugIntent(), request.GetIdempotencyKey(), nil
}

func ProviderReadbackRequest(request *interactiongatewayv1.GetMattermostTeamProviderReadbackRequest) (string, error) {
	if request == nil || !validUUID(request.GetSelector()) {
		return "", errors.New("mattermost team provider readback request is invalid")
	}
	return request.GetSelector(), nil
}

func validUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil
}

package http

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/codex-k8s/codex-k8s/libs/go/errs"
	controlplanev1 "github.com/codex-k8s/codex-k8s/proto/gen/go/codexk8s/controlplane/v1"
	"github.com/codex-k8s/codex-k8s/services/external/api-gateway/internal/transport/http/casters"
	"github.com/codex-k8s/codex-k8s/services/external/api-gateway/internal/transport/http/models"
)

func (h *staffHandler) TransitionIssueStageLabel(c *echo.Context) error {
	return withPrincipal(c, func(principal *controlplanev1.Principal) error {
		var req models.TransitionIssueStageLabelRequest
		if err := bindBody(c, &req); err != nil {
			return err
		}

		repositoryFullName := strings.TrimSpace(req.RepositoryFullName)
		if repositoryFullName == "" {
			return errs.Validation{Field: "repository_full_name", Msg: "is required"}
		}
		issueNumber := int(req.IssueNumber)
		if issueNumber <= 0 {
			return errs.Validation{Field: "issue_number", Msg: "must be a positive integer"}
		}
		targetLabel := strings.TrimSpace(req.TargetLabel)
		if targetLabel == "" {
			return errs.Validation{Field: "target_label", Msg: "is required"}
		}

		resp, err := h.cp.Service().TransitionIssueStageLabel(c.Request().Context(), &controlplanev1.TransitionIssueStageLabelRequest{
			Principal:          principal,
			RepositoryFullName: repositoryFullName,
			IssueNumber:        int32(issueNumber),
			TargetLabel:        targetLabel,
		})
		if err != nil {
			return err
		}

		return c.JSON(http.StatusOK, casters.TransitionIssueStageLabelResponse(resp))
	})
}

func (h *staffHandler) ListDocsetGroups(c *echo.Context) error {
	return withPrincipal(c, func(principal *controlplanev1.Principal) error {
		docsetRef := strings.TrimSpace(c.QueryParam("docset_ref"))
		locale := strings.TrimSpace(c.QueryParam("locale"))
		resp, err := h.cp.Service().ListDocsetGroups(c.Request().Context(), &controlplanev1.ListDocsetGroupsRequest{
			Principal: principal,
			DocsetRef: docsetRef,
			Locale:    locale,
		})
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, models.DocsetGroupItemsResponse{Groups: casters.DocsetGroups(resp.GetGroups())})
	})
}

func (h *staffHandler) ImportDocset(c *echo.Context) error {
	return withPrincipalAndResolved(c, resolvePath("project_id"), func(principal *controlplanev1.Principal, projectID string) error {
		var req models.ImportDocsetRequest
		if err := bindBody(c, &req); err != nil {
			return err
		}
		resp, err := h.cp.Service().ImportDocset(c.Request().Context(), &controlplanev1.ImportDocsetRequest{
			Principal:    principal,
			ProjectId:    projectID,
			RepositoryId: strings.TrimSpace(req.RepositoryID),
			DocsetRef:    strings.TrimSpace(req.DocsetRef),
			Locale:       strings.TrimSpace(req.Locale),
			GroupIds:     req.GroupIDs,
		})
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, casters.ImportDocsetResponse(resp))
	})
}

func (h *staffHandler) SyncDocset(c *echo.Context) error {
	return withPrincipalAndResolved(c, resolvePath("project_id"), func(principal *controlplanev1.Principal, projectID string) error {
		var req models.SyncDocsetRequest
		if err := bindBody(c, &req); err != nil {
			return err
		}
		resp, err := h.cp.Service().SyncDocset(c.Request().Context(), &controlplanev1.SyncDocsetRequest{
			Principal:    principal,
			ProjectId:    projectID,
			RepositoryId: strings.TrimSpace(req.RepositoryID),
			DocsetRef:    strings.TrimSpace(req.DocsetRef),
		})
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, casters.SyncDocsetResponse(resp))
	})
}

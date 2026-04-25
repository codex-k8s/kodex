package http

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/api-gateway/internal/transport/http/casters"
	"github.com/labstack/echo/v5"
)

func (h *staffHandler) GetAccessMembershipGraph(c *echo.Context) error {
	return withPrincipalAndResolved(c, resolveLimit(500), func(principal *controlplanev1.Principal, limit int) error {
		resp, err := h.getAccessMembershipGraphCall(c.Request().Context(), principal, int32(limit))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, casters.AccessMembershipGraph(resp))
	})
}

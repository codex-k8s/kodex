package grpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAssistantCurrentDiagnosticsPreserveStatusAndExportOnlyClosedStage(t *testing.T) {
	kind := controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_CURRENT_CONFIGURATION
	for _, stage := range []string{errs.AssistantCurrentPromptContext, errs.AssistantCurrentConfigurationView, errs.AssistantCurrentOwnerCoreRead,
		errs.AssistantCurrentOwnerCoreVersion, errs.AssistantCurrentTemplateProjection, errs.AssistantCurrentUnclassified} {
		err := assistantCatalogTransportError(kind, errs.WithAssistantCurrentConfigurationStage(errors.Join(errs.ErrUnavailable, errors.New("PRIVATE_PROVIDER_PAYLOAD")), stage))
		value := status.Convert(err)
		if value.Code() != codes.Unavailable || len(value.Details()) != 1 || strings.Contains(value.String(), "PRIVATE_PROVIDER_PAYLOAD") {
			t.Fatal("diagnostic changed status or disclosed private cause")
		}
		info, ok := value.Details()[0].(*errdetails.ErrorInfo)
		if !ok || info.Reason != stage || info.Domain != controlPlaneErrorDomain || len(info.Metadata) != 0 {
			t.Fatal("diagnostic details escaped the closed stage contract")
		}
	}
	for _, err := range []error{nil, errs.ErrNotFound, errs.ErrForbidden, errs.ErrInvalid, context.Canceled, context.DeadlineExceeded, errors.Join(errs.ErrUnavailable, context.Canceled)} {
		actual := assistantCatalogTransportError(kind, errs.WithAssistantCurrentConfigurationStage(err, errs.AssistantCurrentPromptContext))
		if status.Code(actual) != status.Code(transportError(err)) || len(status.Convert(actual).Details()) != 0 {
			t.Fatal("stage diagnostics changed an existing status boundary")
		}
	}
	for _, other := range []controlplanev1.AssistantConfigurationCatalogKind{0, controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_MODELS} {
		if len(status.Convert(assistantCatalogTransportError(other, errs.ErrUnavailable)).Details()) != 0 {
			t.Fatal("own-read stage escaped into another catalog operation")
		}
	}
}

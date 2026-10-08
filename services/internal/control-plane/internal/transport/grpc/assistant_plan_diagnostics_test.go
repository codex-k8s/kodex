package grpc

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAssistantPlanDiagnosticsExportOnlyBoundedMetadata(t *testing.T) {
	for _, stage := range []string{errs.AssistantPlanHydrate, errs.AssistantPlanNormalize, errs.AssistantPlanBind, errs.AssistantPlanAuthorize, errs.AssistantPlanEmpty, errs.AssistantPlanCommand} {
		for _, cause := range []error{errs.ErrConflict, errs.ErrVersionMismatch, errs.ErrInvalid} {
			index := 4
			if stage == errs.AssistantPlanEmpty {
				index = 0
			}
			value := status.Convert(assistantPlanTransportError(errs.WithAssistantPlanStage(errors.Join(cause, errors.New("PRIVATE_OPERATION_PAYLOAD")), stage, index)))
			wantCode := codes.Aborted
			if cause == errs.ErrInvalid {
				wantCode = codes.InvalidArgument
			}
			if value.Code() != wantCode || len(value.Details()) != 1 || strings.Contains(value.String(), "PRIVATE_OPERATION_PAYLOAD") {
				t.Fatal("plan transport changed status or disclosed private data")
			}
			info, ok := value.Details()[0].(*errdetails.ErrorInfo)
			wantCategory := "CONFLICT"
			if cause == errs.ErrVersionMismatch {
				wantCategory = "VERSION"
			} else if cause == errs.ErrInvalid {
				wantCategory = "INVALID"
			}
			if !ok || info.Domain != controlPlaneErrorDomain || info.Reason != stage || info.Metadata["category"] != wantCategory {
				t.Fatal("plan diagnostic was not materialized")
			}
			if index > 0 {
				if len(info.Metadata) != 2 || info.Metadata["operation_index"] != strconv.Itoa(index) {
					t.Fatal("operation index escaped bounded metadata")
				}
			} else if len(info.Metadata) != 1 {
				t.Fatal("empty proposal invented an operation index")
			}
		}
	}
	for _, cause := range []error{nil, errs.ErrConflict, errs.ErrVersionMismatch, errs.ErrUnavailable, errs.ErrForbidden, context.Canceled, errors.Join(errs.ErrConflict, context.DeadlineExceeded)} {
		actual := assistantPlanTransportError(errs.WithAssistantPlanStage(cause, errs.AssistantPlanHydrate, 1))
		if status.Code(actual) != status.Code(transportError(cause)) {
			t.Fatal("diagnostic changed canonical status precedence")
		}
		if status.Code(actual) != codes.Aborted && len(status.Convert(actual).Details()) != 0 {
			t.Fatal("plan metadata escaped its status boundary")
		}
	}
}

func TestAssistantPlanInvalidFieldTransportIsMetadataOnly(t *testing.T) {
	failure := errs.WithAssistantPlanStage(errs.WithAssistantPlanField(errors.Join(errs.ErrInvalid, errors.New("PRIVATE_INPUT")), "STEPS_INSTRUCTIONS"), errs.AssistantPlanHydrate, 1)
	value := status.Convert(assistantPlanTransportError(failure))
	if value.Code() != codes.InvalidArgument || len(value.Details()) != 1 || strings.Contains(value.String(), "PRIVATE_INPUT") {
		t.Fatal("invalid field changed canonical status or exposed input")
	}
	info := value.Details()[0].(*errdetails.ErrorInfo)
	if info.Domain != controlPlaneErrorDomain || info.Reason != errs.AssistantPlanHydrate || len(info.Metadata) != 3 || info.Metadata["category"] != "INVALID" || info.Metadata["field"] != "STEPS_INSTRUCTIONS" || info.Metadata["operation_index"] != "1" {
		t.Fatal("invalid guard metadata was not exported exactly")
	}
	if len(status.Convert(assistantPlanTransportError(errs.ErrInvalid)).Details()) != 0 {
		t.Fatal("bare invalid error invented a guard")
	}
}

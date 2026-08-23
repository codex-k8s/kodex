package grpc

import (
	"bytes"
	"context"
	"io"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	repository "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maximumInlineArtifactBytes = 16 << 20

func (server *Server) UploadArtifact(stream controlplanev1.PlatformCommandService_UploadArtifactServer) error {
	p, err := principal(stream.Context(), controlplanev1.PlatformCommandService_UploadArtifact_FullMethodName)
	if err != nil {
		return err
	}
	first, err := stream.Recv()
	if err != nil {
		return status.Error(codes.InvalidArgument, "artifact metadata is required")
	}
	metadata := first.GetMetadata()
	if metadata == nil || metadata.GetSizeBytes() < 0 || metadata.GetSizeBytes() > maximumInlineArtifactBytes {
		return status.Error(codes.InvalidArgument, "artifact metadata is invalid")
	}
	buffer := bytes.NewBuffer(make([]byte, 0, int(metadata.GetSizeBytes())))
	for {
		part, receiveErr := stream.Recv()
		if receiveErr == io.EOF {
			break
		}
		if receiveErr != nil {
			return receiveErr
		}
		if part.GetMetadata() != nil || len(part.GetChunk()) == 0 {
			return status.Error(codes.InvalidArgument, "artifact stream is invalid")
		}
		if int64(buffer.Len()+len(part.GetChunk())) > metadata.GetSizeBytes() || buffer.Len()+len(part.GetChunk()) > maximumInlineArtifactBytes {
			return status.Error(codes.ResourceExhausted, "artifact size exceeds the declared limit")
		}
		_, _ = buffer.Write(part.GetChunk())
	}
	if int64(buffer.Len()) != metadata.GetSizeBytes() {
		return status.Error(codes.InvalidArgument, "artifact size does not match metadata")
	}
	artifact, err := server.service.UploadArtifact(stream.Context(), p, mutation(metadata.GetMutation()), repository.ArtifactUpload{ProjectRef: metadata.GetProjectRef(), RunRef: metadata.GetRunRef(), FileName: metadata.GetFileName(), MediaType: metadata.GetMediaType(), SizeBytes: metadata.GetSizeBytes(), Reader: bytes.NewReader(buffer.Bytes())})
	if err != nil {
		return transportError(err)
	}
	return stream.SendAndClose(&controlplanev1.UploadArtifactResponse{Artifact: castArtifact(artifact)})
}

func (server *Server) DownloadArtifact(request *controlplanev1.DownloadArtifactRequest, stream controlplanev1.PlatformCommandService_DownloadArtifactServer) error {
	p, err := principal(stream.Context(), controlplanev1.PlatformCommandService_DownloadArtifact_FullMethodName)
	if err != nil {
		return err
	}
	purpose := ""
	switch request.GetPurpose() {
	case controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_DOWNLOAD:
		purpose = "DOWNLOAD"
	case controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_PREVIEW:
		purpose = "PREVIEW"
	default:
		return status.Error(codes.InvalidArgument, "artifact download purpose is required")
	}
	download, err := server.service.DownloadArtifact(stream.Context(), p, request.GetArtifactRef(), purpose)
	if err != nil {
		return transportError(err)
	}
	defer download.Reader.Close()
	if err := stream.Send(&controlplanev1.DownloadArtifactResponse{
		FileName:  download.Artifact.FileName,
		MediaType: download.Artifact.MediaType,
		SizeBytes: download.Artifact.SizeBytes,
	}); err != nil {
		return err
	}
	chunk := make([]byte, 64<<10)
	for {
		count, readErr := download.Reader.Read(chunk)
		if count > 0 {
			response := &controlplanev1.DownloadArtifactResponse{Data: append([]byte(nil), chunk[:count]...)}
			if err := stream.Send(response); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return status.Error(codes.Internal, "artifact stream failed")
		}
	}
}

func (server *Server) ChangeArtifactBinding(ctx context.Context, request *controlplanev1.ChangeArtifactBindingRequest) (*controlplanev1.ChangeArtifactBindingResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ChangeArtifactBinding_FullMethodName, command.ChangeArtifactBinding, request.GetMutation(), command.ArtifactBindingInput{ArtifactRef: request.GetArtifactRef(), AgentRef: request.GetAgentRef(), Enabled: request.GetEnabled()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ChangeArtifactBindingResponse{Artifact: castArtifact(*result.Artifact)}, nil
}

func (server *Server) CreateSchedule(ctx context.Context, request *controlplanev1.CreateScheduleRequest) (*controlplanev1.CreateScheduleResponse, error) {
	payload := command.ScheduleInput{ProjectRef: request.GetProjectRef(), Name: request.GetName(), Target: runTarget(request.GetTarget()), Preset: request.GetPreset(), CronExpression: request.GetCronExpression(), TimeOfDay: request.GetTimeOfDay(), DayOfWeek: request.GetDayOfWeek(), Timezone: request.GetTimezone(), Input: asMap(request.GetInput()), SessionPolicy: request.GetSessionPolicy(), NotificationPolicy: request.GetNotificationPolicy(), Enabled: true}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateSchedule_FullMethodName, command.CreateSchedule, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateScheduleResponse{Schedule: castSchedule(*result.Schedule)}, nil
}

func (server *Server) UpdateSchedule(ctx context.Context, request *controlplanev1.UpdateScheduleRequest) (*controlplanev1.UpdateScheduleResponse, error) {
	payload := command.ScheduleInput{Ref: request.GetScheduleRef(), Name: request.GetName(), Target: runTarget(request.GetTarget()), Preset: request.GetPreset(), CronExpression: request.GetCronExpression(), TimeOfDay: request.GetTimeOfDay(), DayOfWeek: request.GetDayOfWeek(), Timezone: request.GetTimezone(), Input: asMap(request.GetInput()), SessionPolicy: request.GetSessionPolicy(), NotificationPolicy: request.GetNotificationPolicy()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_UpdateSchedule_FullMethodName, command.UpdateSchedule, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UpdateScheduleResponse{Schedule: castSchedule(*result.Schedule)}, nil
}

func (server *Server) SetScheduleEnabled(ctx context.Context, request *controlplanev1.SetScheduleEnabledRequest) (*controlplanev1.SetScheduleEnabledResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_SetScheduleEnabled_FullMethodName, command.SetScheduleEnabled, request.GetMutation(), command.ScheduleInput{Ref: request.GetScheduleRef(), Enabled: request.GetEnabled()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.SetScheduleEnabledResponse{Schedule: castSchedule(*result.Schedule)}, nil
}

func (server *Server) CreateIntegrationConnection(ctx context.Context, request *controlplanev1.CreateIntegrationConnectionRequest) (*controlplanev1.CreateIntegrationConnectionResponse, error) {
	payload := command.ConnectionInput{DefinitionKey: request.GetDefinitionKey(), Name: request.GetName(), PublicConfiguration: asMap(request.GetPublicConfiguration()), Enabled: true}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateIntegrationConnection_FullMethodName, command.CreateConnection, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateIntegrationConnectionResponse{Connection: castConnection(*result.Connection)}, nil
}

func (server *Server) TestIntegrationConnection(ctx context.Context, request *controlplanev1.TestIntegrationConnectionRequest) (*controlplanev1.TestIntegrationConnectionResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_TestIntegrationConnection_FullMethodName, command.TestConnection, request.GetMutation(), command.ConnectionInput{Ref: request.GetConnectionRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.TestIntegrationConnectionResponse{Connection: castConnection(*result.Connection)}, nil
}

func (server *Server) SetIntegrationConnectionEnabled(ctx context.Context, request *controlplanev1.SetIntegrationConnectionEnabledRequest) (*controlplanev1.SetIntegrationConnectionEnabledResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_SetIntegrationConnectionEnabled_FullMethodName, command.SetConnectionEnabled, request.GetMutation(), command.ConnectionInput{Ref: request.GetConnectionRef(), Enabled: request.GetEnabled()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.SetIntegrationConnectionEnabledResponse{Connection: castConnection(*result.Connection)}, nil
}

func (server *Server) ChangeIntegrationGrant(ctx context.Context, request *controlplanev1.ChangeIntegrationGrantRequest) (*controlplanev1.ChangeIntegrationGrantResponse, error) {
	payload := command.IntegrationGrantInput{ConnectionRef: request.GetConnectionRef(), CapabilityKey: request.GetCapabilityKey(), AgentRef: request.GetAgentRef(), WorkflowRef: request.GetWorkflowRef(), Enabled: request.GetEnabled()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ChangeIntegrationGrant_FullMethodName, command.ChangeIntegrationGrant, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ChangeIntegrationGrantResponse{Connection: castConnection(*result.Connection)}, nil
}

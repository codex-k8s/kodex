package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maximumArtifactChunkBytes = 1 << 20

func (server *Server) CreateAttachmentSetDraft(ctx context.Context, request *controlplanev1.CreateAttachmentSetDraftRequest) (*controlplanev1.CreateAttachmentSetDraftResponse, error) {
	payload := command.AttachmentSetDraftInput{ProjectRef: request.GetProjectRef(),
		Purpose: enumSuffix(request.GetPurpose(), "ATTACHMENT_SET_PURPOSE_"), ArtifactRefs: request.GetArtifactRefs()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateAttachmentSetDraft_FullMethodName,
		command.CreateAttachmentSetDraft, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateAttachmentSetDraftResponse{AttachmentSet: castAttachmentSet(*result.AttachmentSet)}, nil
}

func (server *Server) AddAttachmentSetItems(ctx context.Context, request *controlplanev1.AddAttachmentSetItemsRequest) (*controlplanev1.AddAttachmentSetItemsResponse, error) {
	payload := command.AttachmentSetDraftInput{AttachmentSetRef: request.GetAttachmentSetRef(),
		ArtifactRefs: request.GetArtifactRefs(), InsertAfterPosition: request.GetInsertAfterPosition()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_AddAttachmentSetItems_FullMethodName,
		command.AddAttachmentSetItems, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.AddAttachmentSetItemsResponse{AttachmentSet: castAttachmentSet(*result.AttachmentSet)}, nil
}

func (server *Server) RemoveAttachmentSetItems(ctx context.Context, request *controlplanev1.RemoveAttachmentSetItemsRequest) (*controlplanev1.RemoveAttachmentSetItemsResponse, error) {
	payload := command.AttachmentSetDraftInput{AttachmentSetRef: request.GetAttachmentSetRef(), ArtifactRefs: request.GetArtifactRefs()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RemoveAttachmentSetItems_FullMethodName,
		command.RemoveAttachmentSetItems, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RemoveAttachmentSetItemsResponse{AttachmentSet: castAttachmentSet(*result.AttachmentSet)}, nil
}

func (server *Server) FinalizeAttachmentSet(ctx context.Context, request *controlplanev1.FinalizeAttachmentSetRequest) (*controlplanev1.FinalizeAttachmentSetResponse, error) {
	payload := command.AttachmentSetDraftInput{AttachmentSetRef: request.GetAttachmentSetRef()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_FinalizeAttachmentSet_FullMethodName,
		command.FinalizeAttachmentSet, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.FinalizeAttachmentSetResponse{AttachmentSet: castAttachmentSet(*result.AttachmentSet)}, nil
}

func (server *Server) UploadArtifact(stream controlplanev1.PlatformCommandService_UploadArtifactServer) error {
	p, err := principal(stream.Context(), controlplanev1.PlatformCommandService_UploadArtifact_FullMethodName)
	if err != nil {
		return err
	}
	upload, err := receiveArtifactUpload(stream)
	if err != nil {
		return err
	}
	defer upload.close()
	metadata := upload.metadata
	artifact, err := server.service.UploadArtifact(stream.Context(), p, mutation(metadata.GetMutation()), repository.ArtifactUpload{
		ProjectRef: metadata.GetProjectRef(), RunRef: metadata.GetRunRef(), FileName: metadata.GetFileName(),
		MediaType: metadata.GetMediaType(), SizeBytes: metadata.GetSizeBytes(), Digest: "sha256:" + upload.sha256,
		Reader: upload.file,
	})
	if err != nil {
		return transportError(err)
	}
	return stream.SendAndClose(&controlplanev1.UploadArtifactResponse{Artifact: castArtifact(artifact)})
}

type artifactUploadStream interface {
	Recv() (*controlplanev1.UploadArtifactRequest, error)
}

type receivedArtifactUpload struct {
	metadata *controlplanev1.UploadArtifactMetadata
	file     *os.File
	sha256   string
}

func (upload *receivedArtifactUpload) close() {
	if upload == nil || upload.file == nil {
		return
	}
	name := upload.file.Name()
	_ = upload.file.Close()
	_ = os.Remove(name)
}

func receiveArtifactUpload(stream artifactUploadStream) (*receivedArtifactUpload, error) {
	first, err := stream.Recv()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "artifact metadata is required")
	}
	metadata := first.GetMetadata()
	if metadata == nil || metadata.GetSizeBytes() < 0 {
		return nil, status.Error(codes.InvalidArgument, "artifact metadata is invalid")
	}
	if metadata.GetSizeBytes() > repository.MaximumArtifactBytes {
		return nil, status.Error(codes.ResourceExhausted, "artifact size exceeds the declared limit")
	}
	file, err := os.CreateTemp("", "kodex-artifact-upload-*")
	if err != nil {
		return nil, status.Error(codes.Internal, "artifact temporary storage is unavailable")
	}
	upload := &receivedArtifactUpload{metadata: metadata, file: file}
	keep := false
	defer func() {
		if !keep {
			upload.close()
		}
	}()

	digest := sha256.New()
	written := int64(0)
	for {
		part, receiveErr := stream.Recv()
		if receiveErr == io.EOF {
			return nil, status.Error(codes.InvalidArgument, "artifact commit is required")
		}
		if receiveErr != nil {
			return nil, receiveErr
		}
		if commit := part.GetCommit(); commit != nil {
			actualSHA256 := hex.EncodeToString(digest.Sum(nil))
			if written != metadata.GetSizeBytes() || commit.GetSizeBytes() != written ||
				!validSHA256(commit.GetSha256()) || commit.GetSha256() != actualSHA256 {
				return nil, status.Error(codes.InvalidArgument, "artifact size or digest does not match the stream")
			}
			if trailing, trailingErr := stream.Recv(); trailingErr != io.EOF || trailing != nil {
				if trailingErr != nil && trailingErr != io.EOF {
					return nil, trailingErr
				}
				return nil, status.Error(codes.InvalidArgument, "artifact commit must terminate the stream")
			}
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return nil, status.Error(codes.Internal, "artifact temporary storage is unavailable")
			}
			upload.sha256 = actualSHA256
			keep = true
			return upload, nil
		}
		chunk := part.GetChunk()
		if part.GetMetadata() != nil || len(chunk) == 0 {
			return nil, status.Error(codes.InvalidArgument, "artifact stream is invalid")
		}
		if len(chunk) > maximumArtifactChunkBytes || written+int64(len(chunk)) > metadata.GetSizeBytes() ||
			written+int64(len(chunk)) > repository.MaximumArtifactBytes {
			return nil, status.Error(codes.ResourceExhausted, "artifact chunk exceeds the declared limit")
		}
		count, writeErr := io.MultiWriter(file, digest).Write(chunk)
		if writeErr != nil || count != len(chunk) {
			return nil, status.Error(codes.Internal, "artifact temporary storage write failed")
		}
		written += int64(count)
	}
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
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

func (server *Server) DeleteArtifact(ctx context.Context, request *controlplanev1.DeleteArtifactRequest) (*controlplanev1.DeleteArtifactResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_DeleteArtifact_FullMethodName, command.DeleteArtifact, request.GetMutation(), command.ArtifactLifecycleInput{ArtifactRef: request.GetArtifactRef(), ImpactDigest: request.GetImpactDigest()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.DeleteArtifactResponse{Artifact: castArtifact(*result.Artifact)}, nil
}

func (server *Server) RestoreArtifact(ctx context.Context, request *controlplanev1.RestoreArtifactRequest) (*controlplanev1.RestoreArtifactResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RestoreArtifact_FullMethodName, command.RestoreArtifact, request.GetMutation(), command.ArtifactLifecycleInput{ArtifactRef: request.GetArtifactRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RestoreArtifactResponse{Artifact: castArtifact(*result.Artifact)}, nil
}

func (server *Server) PurgeArtifact(ctx context.Context, request *controlplanev1.PurgeArtifactRequest) (*controlplanev1.PurgeArtifactResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformCommandService_PurgeArtifact_FullMethodName)
	if err != nil {
		return nil, err
	}
	state, err := server.service.PurgeArtifact(ctx, p, mutation(request.GetMutation()), request.GetArtifactRef(), request.GetImpactDigest())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.PurgeArtifactResponse{ArtifactRef: request.GetArtifactRef(), LifecycleState: artifactLifecycleState(state)}, nil
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

func (server *Server) ArchiveSchedule(ctx context.Context, request *controlplanev1.ArchiveScheduleRequest) (*controlplanev1.ArchiveScheduleResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ArchiveSchedule_FullMethodName, command.ArchiveSchedule, request.GetMutation(), command.ScheduleInput{Ref: request.GetScheduleRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ArchiveScheduleResponse{Schedule: castSchedule(*result.Schedule)}, nil
}

func (server *Server) CreateIntegrationConnection(ctx context.Context, request *controlplanev1.CreateIntegrationConnectionRequest) (*controlplanev1.CreateIntegrationConnectionResponse, error) {
	payload := command.ConnectionInput{DefinitionKey: request.GetDefinitionKey(), Name: request.GetName(), PublicConfiguration: asMap(request.GetPublicConfiguration()), Enabled: true}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateIntegrationConnection_FullMethodName, command.CreateConnection, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateIntegrationConnectionResponse{Connection: castConnection(*result.Connection)}, nil
}

func (server *Server) ConfigureIntegrationConnectionCredential(ctx context.Context, request *controlplanev1.ConfigureIntegrationConnectionCredentialRequest) (*controlplanev1.ConfigureIntegrationConnectionCredentialResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformCommandService_ConfigureIntegrationConnectionCredential_FullMethodName)
	if err != nil {
		return nil, err
	}
	connection, err := server.service.ConfigureIntegrationCredential(ctx, p, mutation(request.GetMutation()), request.GetConnectionRef(), request.GetCredentialValue())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ConfigureIntegrationConnectionCredentialResponse{Connection: castConnection(connection)}, nil
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

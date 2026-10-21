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

// maximumArtifactChunkBytes bounds each received upload frame independently of
// the aggregate artifact size limit before bytes reach temporary storage.
// maximumArtifactChunkBytes ограничивает каждый кадр загрузки независимо от
// общего лимита артефакта до записи во временное хранилище.
const maximumArtifactChunkBytes = 1 << 20

// executeAttachmentSetMutation runs a typed attachment-set command through the
// shared mutation boundary and converts only its resulting set for the RPC.
// executeAttachmentSetMutation проводит типизированную команду набора вложений
// через общую границу мутаций и преобразует только итоговый набор для RPC.
func (server *Server) executeAttachmentSetMutation(ctx context.Context, method string, kind command.Kind, mutation *controlplanev1.MutationContext, payload command.AttachmentSetDraftInput) (*controlplanev1.AttachmentSet, error) {
	result, err := execute(ctx, server.service, method, kind, mutation, payload)
	if err != nil {
		return nil, err
	}
	return castAttachmentSet(*result.AttachmentSet), nil
}

// CreateAttachmentSetDraft validates project scope and creates a mutable set
// draft; the domain command remains responsible for authorization and state.
// CreateAttachmentSetDraft проверяет область проекта и создаёт черновик набора;
// доменная команда по-прежнему отвечает за полномочия и состояние.
func (server *Server) CreateAttachmentSetDraft(ctx context.Context, request *controlplanev1.CreateAttachmentSetDraftRequest) (*controlplanev1.CreateAttachmentSetDraftResponse, error) {
	if request.GetProjectRef() == "" {
		return nil, status.Error(codes.InvalidArgument, "attachment set project scope is required")
	}
	payload := command.AttachmentSetDraftInput{ProjectRef: request.GetProjectRef(),
		Purpose: enumSuffix(request.GetPurpose(), "ATTACHMENT_SET_PURPOSE_"), ArtifactRefs: request.GetArtifactRefs()}
	attachmentSet, err := server.executeAttachmentSetMutation(ctx, controlplanev1.PlatformCommandService_CreateAttachmentSetDraft_FullMethodName,
		command.CreateAttachmentSetDraft, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateAttachmentSetDraftResponse{AttachmentSet: attachmentSet}, nil
}

// CreateOrganizationAttachmentSetDraft creates a draft without a project scope
// while preserving the organization command's own authorization boundary.
// CreateOrganizationAttachmentSetDraft создаёт черновик без области проекта,
// сохраняя отдельную проверку полномочий организационной команды.
func (server *Server) CreateOrganizationAttachmentSetDraft(ctx context.Context, request *controlplanev1.CreateOrganizationAttachmentSetDraftRequest) (*controlplanev1.CreateOrganizationAttachmentSetDraftResponse, error) {
	payload := command.AttachmentSetDraftInput{
		Purpose: enumSuffix(request.GetPurpose(), "ATTACHMENT_SET_PURPOSE_"), ArtifactRefs: request.GetArtifactRefs()}
	attachmentSet, err := server.executeAttachmentSetMutation(ctx, controlplanev1.PlatformCommandService_CreateOrganizationAttachmentSetDraft_FullMethodName,
		command.CreateAttachmentSetDraft, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateOrganizationAttachmentSetDraftResponse{AttachmentSet: attachmentSet}, nil
}

// AddAttachmentSetItems inserts the requested artifacts at the supplied
// position through the versioned attachment-set mutation command.
// AddAttachmentSetItems вставляет указанные артефакты в заданную позицию через
// версионируемую команду изменения набора вложений.
func (server *Server) AddAttachmentSetItems(ctx context.Context, request *controlplanev1.AddAttachmentSetItemsRequest) (*controlplanev1.AddAttachmentSetItemsResponse, error) {
	payload := command.AttachmentSetDraftInput{AttachmentSetRef: request.GetAttachmentSetRef(),
		ArtifactRefs: request.GetArtifactRefs(), InsertAfterPosition: request.GetInsertAfterPosition()}
	attachmentSet, err := server.executeAttachmentSetMutation(ctx, controlplanev1.PlatformCommandService_AddAttachmentSetItems_FullMethodName,
		command.AddAttachmentSetItems, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.AddAttachmentSetItemsResponse{AttachmentSet: attachmentSet}, nil
}

// RemoveAttachmentSetItems removes the named artifacts through the domain
// command and returns the resulting attachment-set snapshot.
// RemoveAttachmentSetItems удаляет названные артефакты доменной командой и
// возвращает получившийся снимок набора вложений.
func (server *Server) RemoveAttachmentSetItems(ctx context.Context, request *controlplanev1.RemoveAttachmentSetItemsRequest) (*controlplanev1.RemoveAttachmentSetItemsResponse, error) {
	payload := command.AttachmentSetDraftInput{AttachmentSetRef: request.GetAttachmentSetRef(), ArtifactRefs: request.GetArtifactRefs()}
	attachmentSet, err := server.executeAttachmentSetMutation(ctx, controlplanev1.PlatformCommandService_RemoveAttachmentSetItems_FullMethodName,
		command.RemoveAttachmentSetItems, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RemoveAttachmentSetItemsResponse{AttachmentSet: attachmentSet}, nil
}

// FinalizeAttachmentSet closes the mutable draft using the domain command and
// returns the finalized attachment-set snapshot to its caller.
// FinalizeAttachmentSet закрывает изменяемый черновик доменной командой и
// возвращает вызывающему итоговый снимок набора вложений.
func (server *Server) FinalizeAttachmentSet(ctx context.Context, request *controlplanev1.FinalizeAttachmentSetRequest) (*controlplanev1.FinalizeAttachmentSetResponse, error) {
	payload := command.AttachmentSetDraftInput{AttachmentSetRef: request.GetAttachmentSetRef()}
	attachmentSet, err := server.executeAttachmentSetMutation(ctx, controlplanev1.PlatformCommandService_FinalizeAttachmentSet_FullMethodName,
		command.FinalizeAttachmentSet, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.FinalizeAttachmentSetResponse{AttachmentSet: attachmentSet}, nil
}

// UploadArtifact receives a project-scoped artifact using the shared streaming
// validator; the authenticated actor is resolved after the metadata frame.
// UploadArtifact принимает артефакт проекта через общую проверку потока;
// подтверждённый actor определяется после кадра метаданных.
func (server *Server) UploadArtifact(stream controlplanev1.PlatformCommandService_UploadArtifactServer) error {
	return server.uploadArtifact(stream, controlplanev1.PlatformCommandService_UploadArtifact_FullMethodName, true)
}

// UploadAgentAvatar validates the same byte stream as an artifact upload but
// requires an agent, project and expected version before the domain mutation.
// UploadAgentAvatar проверяет байтовый поток как загрузку артефакта, но требует
// сотрудника, проект и ожидаемую версию перед доменной мутацией.
func (server *Server) UploadAgentAvatar(stream controlplanev1.PlatformCommandService_UploadAgentAvatarServer) error {
	adapter := &agentAvatarUploadAdapter{stream: stream}
	upload, err := receiveArtifactUpload(adapter)
	if err != nil {
		return err
	}
	defer upload.close()
	// Как и для обычного UploadArtifact, actor client-stream появляется в
	// контексте только после первой metadata-части, принятой interceptor-ом.
	p, err := principal(stream.Context(), controlplanev1.PlatformCommandService_UploadAgentAvatar_FullMethodName)
	if err != nil {
		return err
	}
	metadata := adapter.metadata
	if metadata == nil || metadata.GetProjectRef() == "" || metadata.GetAgentRef() == "" ||
		metadata.GetMutation() == nil || metadata.GetMutation().ExpectedVersion == nil || metadata.GetSizeBytes() > 5<<20 {
		return status.Error(codes.InvalidArgument, "agent avatar upload metadata is invalid")
	}
	agent, err := server.service.UploadAgentAvatar(stream.Context(), p, mutation(metadata.GetMutation()), repository.AgentAvatarUpload{
		ArtifactUpload: repository.ArtifactUpload{
			ProjectRef: metadata.GetProjectRef(), FileName: metadata.GetFileName(), MediaType: metadata.GetMediaType(),
			SizeBytes: metadata.GetSizeBytes(), Digest: "sha256:" + upload.sha256, Reader: upload.file,
		},
		AgentRef: metadata.GetAgentRef(), ExpectedVersion: metadata.GetMutation().GetExpectedVersion(),
	})
	if err != nil {
		return transportError(err)
	}
	return stream.SendAndClose(&controlplanev1.UploadAgentAvatarResponse{Agent: castAgent(agent)})
}

// agentAvatarUploadAdapter maps avatar-specific frames into the common upload
// protocol while retaining the original metadata for agent authorization.
// agentAvatarUploadAdapter преобразует кадры аватара в общий протокол загрузки,
// сохраняя исходные метаданные для проверки полномочий сотрудника.
type agentAvatarUploadAdapter struct {
	stream   controlplanev1.PlatformCommandService_UploadAgentAvatarServer
	metadata *controlplanev1.UploadAgentAvatarMetadata
}

// Recv converts one avatar frame and records its metadata without changing
// stream order or interpreting the uploaded bytes as trusted content.
// Recv преобразует один кадр аватара и сохраняет метаданные без изменения
// порядка потока и без признания загруженных байтов доверенным содержимым.
func (adapter *agentAvatarUploadAdapter) Recv() (*controlplanev1.UploadArtifactRequest, error) {
	request, err := adapter.stream.Recv()
	if err != nil {
		return nil, err
	}
	converted := &controlplanev1.UploadArtifactRequest{}
	switch part := request.GetPart().(type) {
	case *controlplanev1.UploadAgentAvatarRequest_Metadata:
		adapter.metadata = part.Metadata
		if part.Metadata != nil {
			converted.Part = &controlplanev1.UploadArtifactRequest_Metadata{Metadata: &controlplanev1.UploadArtifactMetadata{
				Mutation: part.Metadata.GetMutation(), ProjectRef: part.Metadata.GetProjectRef(),
				FileName: part.Metadata.GetFileName(), MediaType: part.Metadata.GetMediaType(), SizeBytes: part.Metadata.GetSizeBytes(),
			}}
		}
	case *controlplanev1.UploadAgentAvatarRequest_Chunk:
		converted.Part = &controlplanev1.UploadArtifactRequest_Chunk{Chunk: part.Chunk}
	case *controlplanev1.UploadAgentAvatarRequest_Commit:
		converted.Part = &controlplanev1.UploadArtifactRequest_Commit{Commit: part.Commit}
	}
	return converted, nil
}

// UploadOrganizationArtifact accepts organization-scoped bytes through the
// common upload validator, with project and run references forbidden.
// UploadOrganizationArtifact принимает байты организации через общую проверку
// загрузки, запрещая ссылки на проект и запуск.
func (server *Server) UploadOrganizationArtifact(stream controlplanev1.PlatformCommandService_UploadOrganizationArtifactServer) error {
	return server.uploadArtifact(organizationArtifactUploadServer{stream: stream}, controlplanev1.PlatformCommandService_UploadOrganizationArtifact_FullMethodName, false)
}

// organizationArtifactUploadServer adapts the organization RPC stream to the
// common artifact upload interface without widening its response type.
// organizationArtifactUploadServer приводит поток организационного RPC к
// общему интерфейсу загрузки, не расширяя тип его ответа.
type organizationArtifactUploadServer struct {
	stream controlplanev1.PlatformCommandService_UploadOrganizationArtifactServer
}

// Context forwards the original stream context so authentication remains
// bound to the organization RPC rather than a fabricated local context.
// Context передаёт исходный контекст потока, сохраняя привязку авторизации к
// организационному RPC вместо созданного локально контекста.
func (server organizationArtifactUploadServer) Context() context.Context {
	return server.stream.Context()
}

// Recv converts organization-specific upload frames to the common framing
// while preserving the metadata, chunk and commit sequence.
// Recv преобразует организационные кадры загрузки в общий формат, сохраняя
// последовательность метаданных, фрагментов и подтверждения.
func (server organizationArtifactUploadServer) Recv() (*controlplanev1.UploadArtifactRequest, error) {
	request, err := server.stream.Recv()
	if err != nil {
		return nil, err
	}
	converted := &controlplanev1.UploadArtifactRequest{}
	switch part := request.GetPart().(type) {
	case *controlplanev1.UploadOrganizationArtifactRequest_Metadata:
		converted.Part = &controlplanev1.UploadArtifactRequest_Metadata{Metadata: part.Metadata}
	case *controlplanev1.UploadOrganizationArtifactRequest_Chunk:
		converted.Part = &controlplanev1.UploadArtifactRequest_Chunk{Chunk: part.Chunk}
	case *controlplanev1.UploadOrganizationArtifactRequest_Commit:
		converted.Part = &controlplanev1.UploadArtifactRequest_Commit{Commit: part.Commit}
	}
	return converted, nil
}

// SendAndClose wraps the validated artifact in one organization-specific RPC
// response and completes the original client stream.
// SendAndClose помещает проверенный артефакт в один ответ организационного RPC
// и завершает исходный клиентский поток.
func (server organizationArtifactUploadServer) SendAndClose(response *controlplanev1.UploadArtifactResponse) error {
	return server.stream.SendAndClose(&controlplanev1.UploadOrganizationArtifactResponse{Artifact: response.GetArtifact()})
}

// artifactUploadServer combines common upload frames with the authenticated
// RPC context and the typed completion callback needed by uploadArtifact.
// artifactUploadServer объединяет общие кадры загрузки с контекстом
// авторизованного RPC и типизированным завершением для uploadArtifact.
type artifactUploadServer interface {
	artifactUploadStream
	// Context returns the RPC stream context after its first metadata frame has
	// established the authenticated principal for this exact method.
	// Context возвращает контекст RPC-потока после кадра метаданных, который
	// устанавливает подтверждённого principal для этого метода.
	Context() context.Context
	// SendAndClose emits the typed artifact completion response on the original
	// client stream after domain persistence succeeds.
	// SendAndClose выдаёт типизированный ответ о завершении в исходный поток
	// клиента после успешного сохранения доменным сервисом.
	SendAndClose(*controlplanev1.UploadArtifactResponse) error
}

// uploadArtifact validates the byte stream before resolving the actor and
// invokes the domain service only after scope and digest checks succeed.
// uploadArtifact проверяет байтовый поток до определения actor и вызывает
// доменный сервис только после проверки области и дайджеста.
func (server *Server) uploadArtifact(stream artifactUploadServer, fullMethod string, projectRequired bool) error {
	upload, err := receiveArtifactUpload(stream)
	if err != nil {
		return err
	}
	defer upload.close()
	// В service-v1 principal client-stream связывается с первым metadata
	// сообщением. До первого Recv контекст намеренно ещё не содержит actor.
	p, err := principal(stream.Context(), fullMethod)
	if err != nil {
		return err
	}
	metadata := upload.metadata
	if err := validateArtifactUploadScope(metadata, projectRequired); err != nil {
		return err
	}
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

// validateArtifactUploadScope rejects absent metadata and prevents a request
// from mixing project-scoped and organization-scoped upload identifiers.
// validateArtifactUploadScope отклоняет отсутствующие метаданные и не даёт
// смешивать идентификаторы проектной и организационной загрузки.
func validateArtifactUploadScope(metadata *controlplanev1.UploadArtifactMetadata, projectRequired bool) error {
	if metadata == nil || projectRequired && metadata.GetProjectRef() == "" ||
		!projectRequired && (metadata.GetProjectRef() != "" || metadata.GetRunRef() != "") {
		return status.Error(codes.InvalidArgument, "artifact upload scope is invalid")
	}
	return nil
}

// artifactUploadStream exposes one upload frame at a time so the byte validator
// can operate independently of each concrete generated RPC stream.
// artifactUploadStream предоставляет кадры по одному, чтобы проверка байтов
// не зависела от конкретного сгенерированного RPC-потока.
type artifactUploadStream interface {
	// Recv obtains the next metadata, byte chunk or commit frame in wire order;
	// validation is performed by receiveArtifactUpload, not by this interface.
	// Recv получает следующий кадр метаданных, байтов или commit в порядке сети;
	// проверку выполняет receiveArtifactUpload, а не этот интерфейс.
	Recv() (*controlplanev1.UploadArtifactRequest, error)
}

// receivedArtifactUpload owns the verified temporary file and its digest until
// the caller closes and removes it after the domain service consumes it.
// receivedArtifactUpload владеет проверенным временным файлом и дайджестом до
// закрытия и удаления после передачи данных доменному сервису.
type receivedArtifactUpload struct {
	metadata *controlplanev1.UploadArtifactMetadata
	file     *os.File
	sha256   string
}

// close releases and removes the temporary upload file; repeated or nil
// cleanup calls are safe and do not change domain state.
// close закрывает и удаляет временный файл загрузки; повторный вызов или nil
// безопасны и не изменяют доменное состояние.
func (upload *receivedArtifactUpload) close() {
	if upload == nil || upload.file == nil {
		return
	}
	name := upload.file.Name()
	_ = upload.file.Close()
	_ = os.Remove(name)
}

// receiveArtifactUpload spools a bounded stream, hashes its exact bytes, and
// requires a matching terminal commit before returning a rewound file.
// receiveArtifactUpload сохраняет ограниченный поток, хеширует точные байты и
// требует совпадающий завершающий commit до возврата файла с начальной позиции.
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

// validSHA256 accepts only a lowercase hex digest of the required SHA-256
// length, preventing ambiguous commit representations.
// validSHA256 принимает только строчный hex-дайджест требуемой длины SHA-256,
// исключая неоднозначные представления commit.
func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

// DownloadArtifact streams the current authorized artifact through the common
// downloader, preserving its full RPC method for principal resolution.
// DownloadArtifact передаёт текущий разрешённый артефакт через общий путь,
// сохраняя полный RPC-метод для определения principal.
func (server *Server) DownloadArtifact(request *controlplanev1.DownloadArtifactRequest, stream controlplanev1.PlatformCommandService_DownloadArtifactServer) error {
	return server.downloadArtifact(request, stream.Context(), controlplanev1.PlatformCommandService_DownloadArtifact_FullMethodName, "", stream.Send)
}

// DownloadArtifactRevision requires an explicit revision and adapts common
// download frames to the revision-specific response without changing bytes.
// DownloadArtifactRevision требует явную ревизию и преобразует общие кадры в
// ответ конкретной ревизии без изменения байтов.
func (server *Server) DownloadArtifactRevision(request *controlplanev1.DownloadArtifactRevisionRequest, stream controlplanev1.PlatformCommandService_DownloadArtifactRevisionServer) error {
	if request.GetRevisionRef() == "" {
		return status.Error(codes.InvalidArgument, "artifact revision is required")
	}
	return server.downloadArtifact(&controlplanev1.DownloadArtifactRequest{ArtifactRef: request.GetArtifactRef(), Purpose: request.GetPurpose()},
		stream.Context(), controlplanev1.PlatformCommandService_DownloadArtifactRevision_FullMethodName, request.GetRevisionRef(),
		func(frame *controlplanev1.DownloadArtifactResponse) error {
			return stream.Send(&controlplanev1.DownloadArtifactRevisionResponse{Data: frame.GetData(), FileName: frame.GetFileName(), MediaType: frame.GetMediaType(), SizeBytes: frame.GetSizeBytes()})
		})
}

// downloadArtifact validates the purpose, resolves the caller, and streams
// metadata then file chunks from the authorized current or pinned revision.
// downloadArtifact проверяет назначение, определяет вызывающего и передаёт
// метаданные, затем части файла текущей или закреплённой разрешённой ревизии.
func (server *Server) downloadArtifact(request *controlplanev1.DownloadArtifactRequest, ctx context.Context, method, revisionRef string, send func(*controlplanev1.DownloadArtifactResponse) error) error {
	p, err := principal(ctx, method)
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
	var download repository.ArtifactDownload
	if revisionRef == "" {
		download, err = server.service.DownloadArtifact(ctx, p, request.GetArtifactRef(), purpose)
	} else {
		download, err = server.service.DownloadArtifactRevision(ctx, p, request.GetArtifactRef(), revisionRef, purpose)
	}
	if err != nil {
		return artifactDownloadTransportError(err)
	}
	defer download.Reader.Close()
	if err := send(&controlplanev1.DownloadArtifactResponse{
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
			if err := send(response); err != nil {
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

// ChangeArtifactBinding applies the requested agent-artifact binding through
// the versioned domain command and returns the changed artifact snapshot.
// ChangeArtifactBinding применяет связь сотрудника с артефактом через
// версионируемую доменную команду и возвращает изменённый снимок артефакта.
func (server *Server) ChangeArtifactBinding(ctx context.Context, request *controlplanev1.ChangeArtifactBindingRequest) (*controlplanev1.ChangeArtifactBindingResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ChangeArtifactBinding_FullMethodName, command.ChangeArtifactBinding, request.GetMutation(), command.ArtifactBindingInput{ArtifactRef: request.GetArtifactRef(), AgentRef: request.GetAgentRef(), Enabled: request.GetEnabled()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ChangeArtifactBindingResponse{Artifact: castArtifact(*result.Artifact)}, nil
}

// DeleteArtifact requests a reversible lifecycle transition using the supplied
// impact digest; the domain service verifies ownership and mutation version.
// DeleteArtifact запрашивает обратимый переход с указанным дайджестом влияния;
// доменный сервис проверяет владение и версию мутации.
func (server *Server) DeleteArtifact(ctx context.Context, request *controlplanev1.DeleteArtifactRequest) (*controlplanev1.DeleteArtifactResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_DeleteArtifact_FullMethodName, command.DeleteArtifact, request.GetMutation(), command.ArtifactLifecycleInput{ArtifactRef: request.GetArtifactRef(), ImpactDigest: request.GetImpactDigest()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.DeleteArtifactResponse{Artifact: castArtifact(*result.Artifact)}, nil
}

// RestoreArtifact delegates restoration to the versioned domain command and
// returns the resulting artifact rather than changing storage in transport.
// RestoreArtifact передаёт восстановление версионируемой доменной команде и
// возвращает результат, не изменяя хранилище из transport-слоя.
func (server *Server) RestoreArtifact(ctx context.Context, request *controlplanev1.RestoreArtifactRequest) (*controlplanev1.RestoreArtifactResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RestoreArtifact_FullMethodName, command.RestoreArtifact, request.GetMutation(), command.ArtifactLifecycleInput{ArtifactRef: request.GetArtifactRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RestoreArtifactResponse{Artifact: castArtifact(*result.Artifact)}, nil
}

// PurgeArtifact resolves the authenticated caller and delegates irreversible
// artifact removal with the mutation context and impact digest to the service.
// PurgeArtifact определяет подтверждённого вызывающего и передаёт сервису
// безвозвратное удаление с контекстом мутации и дайджестом влияния.
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

// CreateSchedule maps the requested timing, target and policies into an
// enabled domain schedule command without deciding execution authority here.
// CreateSchedule переносит время, цель и политики запроса в команду создания
// включённого расписания, не определяя здесь полномочия на выполнение.
func (server *Server) CreateSchedule(ctx context.Context, request *controlplanev1.CreateScheduleRequest) (*controlplanev1.CreateScheduleResponse, error) {
	payload := command.ScheduleInput{ProjectRef: request.GetProjectRef(), Name: request.GetName(), Target: runTarget(request.GetTarget()), Preset: request.GetPreset(), CronExpression: request.GetCronExpression(), TimeOfDay: request.GetTimeOfDay(), DayOfWeek: request.GetDayOfWeek(), Timezone: request.GetTimezone(), Input: asMap(request.GetInput()), SessionPolicy: request.GetSessionPolicy(), NotificationPolicy: request.GetNotificationPolicy(), DSTGapPolicy: request.GetDstGapPolicy(), DSTFoldPolicy: request.GetDstFoldPolicy(), MisfirePolicy: request.GetMisfirePolicy(), OverlapPolicy: request.GetOverlapPolicy(), AutomationText: request.GetAutomationText(), PromptInputs: asMap(request.GetPromptInputs()), Enabled: true}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateSchedule_FullMethodName, command.CreateSchedule, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateScheduleResponse{Schedule: castSchedule(*result.Schedule)}, nil
}

// UpdateSchedule maps editable timing and execution policy fields for one
// referenced schedule while the domain command enforces its version.
// UpdateSchedule передаёт изменяемые поля времени и политики выполнения
// указанного расписания; доменная команда проверяет его версию.
func (server *Server) UpdateSchedule(ctx context.Context, request *controlplanev1.UpdateScheduleRequest) (*controlplanev1.UpdateScheduleResponse, error) {
	payload := command.ScheduleInput{Ref: request.GetScheduleRef(), Name: request.GetName(), Target: runTarget(request.GetTarget()), Preset: request.GetPreset(), CronExpression: request.GetCronExpression(), TimeOfDay: request.GetTimeOfDay(), DayOfWeek: request.GetDayOfWeek(), Timezone: request.GetTimezone(), Input: asMap(request.GetInput()), SessionPolicy: request.GetSessionPolicy(), NotificationPolicy: request.GetNotificationPolicy(), DSTGapPolicy: request.GetDstGapPolicy(), DSTFoldPolicy: request.GetDstFoldPolicy(), MisfirePolicy: request.GetMisfirePolicy(), OverlapPolicy: request.GetOverlapPolicy(), AutomationText: request.GetAutomationText(), PromptInputs: asMap(request.GetPromptInputs())}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_UpdateSchedule_FullMethodName, command.UpdateSchedule, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UpdateScheduleResponse{Schedule: castSchedule(*result.Schedule)}, nil
}

// SetScheduleEnabled changes only a schedule's enabled flag through its
// dedicated versioned command and returns the resulting schedule snapshot.
// SetScheduleEnabled меняет только признак включения расписания отдельной
// версионируемой командой и возвращает итоговый снимок.
func (server *Server) SetScheduleEnabled(ctx context.Context, request *controlplanev1.SetScheduleEnabledRequest) (*controlplanev1.SetScheduleEnabledResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_SetScheduleEnabled_FullMethodName, command.SetScheduleEnabled, request.GetMutation(), command.ScheduleInput{Ref: request.GetScheduleRef(), Enabled: request.GetEnabled()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.SetScheduleEnabledResponse{Schedule: castSchedule(*result.Schedule)}, nil
}

// ArchiveSchedule delegates the schedule's archive transition to its
// specialized domain command, keeping lifecycle decisions outside transport.
// ArchiveSchedule передаёт переход расписания в архив специализированной
// доменной команде, не принимая lifecycle-решение в transport-слое.
func (server *Server) ArchiveSchedule(ctx context.Context, request *controlplanev1.ArchiveScheduleRequest) (*controlplanev1.ArchiveScheduleResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ArchiveSchedule_FullMethodName, command.ArchiveSchedule, request.GetMutation(), command.ScheduleInput{Ref: request.GetScheduleRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ArchiveScheduleResponse{Schedule: castSchedule(*result.Schedule)}, nil
}

// DeleteSchedule invokes the dedicated schedule deletion command with its
// mutation context and returns the authoritative resulting snapshot.
// DeleteSchedule вызывает отдельную команду удаления расписания с контекстом
// мутации и возвращает авторитетный итоговый снимок.
func (server *Server) DeleteSchedule(ctx context.Context, request *controlplanev1.DeleteScheduleRequest) (*controlplanev1.DeleteScheduleResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_DeleteSchedule_FullMethodName, command.DeleteSchedule, request.GetMutation(), command.ScheduleInput{Ref: request.GetScheduleRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.DeleteScheduleResponse{Schedule: castSchedule(*result.Schedule)}, nil
}

// CreateIntegrationConnection creates an enabled connection from public
// configuration only; credential setup follows a separate protected RPC.
// CreateIntegrationConnection создаёт включённое подключение только из
// публичной конфигурации; реквизиты задаются отдельным защищённым RPC.
func (server *Server) CreateIntegrationConnection(ctx context.Context, request *controlplanev1.CreateIntegrationConnectionRequest) (*controlplanev1.CreateIntegrationConnectionResponse, error) {
	payload := command.ConnectionInput{DefinitionKey: request.GetDefinitionKey(), Name: request.GetName(), PublicConfiguration: asMap(request.GetPublicConfiguration()), Enabled: true}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateIntegrationConnection_FullMethodName, command.CreateConnection, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateIntegrationConnectionResponse{Connection: castConnection(*result.Connection)}, nil
}

// UpdateIntegrationConnection changes a referenced connection's public fields
// through the versioned command without accepting credential values.
// UpdateIntegrationConnection меняет публичные поля указанного подключения
// версионируемой командой, не принимая значения реквизитов.
func (server *Server) UpdateIntegrationConnection(ctx context.Context, request *controlplanev1.UpdateIntegrationConnectionRequest) (*controlplanev1.UpdateIntegrationConnectionResponse, error) {
	payload := command.ConnectionInput{Ref: request.GetConnectionRef(), Name: request.GetName(), PublicConfiguration: asMap(request.GetPublicConfiguration())}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_UpdateIntegrationConnection_FullMethodName, command.UpdateConnection, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UpdateIntegrationConnectionResponse{Connection: castConnection(*result.Connection)}, nil
}

// DeleteIntegrationConnection delegates connection removal to the domain
// command after the shared mutation boundary has resolved the caller.
// DeleteIntegrationConnection передаёт удаление подключения доменной команде
// после определения вызывающего общей границей мутаций.
func (server *Server) DeleteIntegrationConnection(ctx context.Context, request *controlplanev1.DeleteIntegrationConnectionRequest) (*controlplanev1.DeleteIntegrationConnectionResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_DeleteIntegrationConnection_FullMethodName, command.DeleteConnection, request.GetMutation(), command.ConnectionInput{Ref: request.GetConnectionRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.DeleteIntegrationConnectionResponse{Connection: castConnection(*result.Connection)}, nil
}

// ConfigureIntegrationConnectionCredential passes a credential only to the
// protected service operation and returns a public connection projection.
// ConfigureIntegrationConnectionCredential передаёт реквизит только защищённой
// операции сервиса и возвращает публичную проекцию подключения.
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

// TestIntegrationConnection requests a connection check through its domain
// command and returns the connection state recorded by that command.
// TestIntegrationConnection запрашивает проверку подключения доменной командой
// и возвращает записанное ею состояние подключения.
func (server *Server) TestIntegrationConnection(ctx context.Context, request *controlplanev1.TestIntegrationConnectionRequest) (*controlplanev1.TestIntegrationConnectionResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_TestIntegrationConnection_FullMethodName, command.TestConnection, request.GetMutation(), command.ConnectionInput{Ref: request.GetConnectionRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.TestIntegrationConnectionResponse{Connection: castConnection(*result.Connection)}, nil
}

// SetIntegrationConnectionEnabled changes only a connection's enabled flag
// through the dedicated command and preserves its mutation/version checks.
// SetIntegrationConnectionEnabled меняет только признак включения подключения
// отдельной командой с прежними проверками мутации и версии.
func (server *Server) SetIntegrationConnectionEnabled(ctx context.Context, request *controlplanev1.SetIntegrationConnectionEnabledRequest) (*controlplanev1.SetIntegrationConnectionEnabledResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_SetIntegrationConnectionEnabled_FullMethodName, command.SetConnectionEnabled, request.GetMutation(), command.ConnectionInput{Ref: request.GetConnectionRef(), Enabled: request.GetEnabled()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.SetIntegrationConnectionEnabledResponse{Connection: castConnection(*result.Connection)}, nil
}

// ChangeIntegrationGrant maps a connection capability grant and approval
// policy to its own versioned command; the server does not grant itself rights.
// ChangeIntegrationGrant переносит разрешение capability подключения и
// политику согласования в отдельную версионируемую команду без самовыдачи прав.
func (server *Server) ChangeIntegrationGrant(ctx context.Context, request *controlplanev1.ChangeIntegrationGrantRequest) (*controlplanev1.ChangeIntegrationGrantResponse, error) {
	payload := command.IntegrationGrantInput{ConnectionRef: request.GetConnectionRef(), CapabilityKey: request.GetCapabilityKey(), AgentRef: request.GetAgentRef(), WorkflowRef: request.GetWorkflowRef(), Enabled: request.GetEnabled(), ApprovalScopePaths: request.GetApprovalScopePaths(), ApprovalPolicy: strings.TrimPrefix(request.GetApprovalPolicy().String(), "INTEGRATION_APPROVAL_POLICY_")}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ChangeIntegrationGrant_FullMethodName, command.ChangeIntegrationGrant, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ChangeIntegrationGrantResponse{Connection: castConnection(*result.Connection)}, nil
}

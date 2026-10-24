package grpc

import (
	"context"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// ConfigureEmailMailboxCredential passes the credential value only to the authorized owner
// configuration call with mutation and connection, returning a descriptor without the value.
// ConfigureEmailMailboxCredential передаёт значение credential только авторизованному вызову
// настройки владельца с mutation и подключением, возвращая описатель без значения.
func (server *Server) ConfigureEmailMailboxCredential(ctx context.Context, request *controlplanev1.ConfigureEmailMailboxCredentialRequest) (*controlplanev1.ConfigureEmailMailboxCredentialResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformCommandService_ConfigureEmailMailboxCredential_FullMethodName)
	if err != nil {
		return nil, err
	}
	kind := strings.TrimPrefix(request.GetKind().String(), "EMAIL_MAILBOX_CREDENTIAL_KIND_")
	credential, err := server.service.ConfigureEmailMailboxCredential(ctx, p, mutation(request.GetMutation()), request.GetConnectionRef(), kind, request.GetCredentialValue())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ConfigureEmailMailboxCredentialResponse{Credential: castEmailMailboxCredential(credential)}, nil
}

// castEmailMailboxCredential projects credential name, generation, connection/version and kind
// metadata without serializing its stored secret value.
// castEmailMailboxCredential преобразует имя, поколение, подключение с версией и вид credential
// без сериализации сохранённого секретного значения.
func castEmailMailboxCredential(credential entity.EmailMailboxCredential) *controlplanev1.EmailMailboxCredential {
	return &controlplanev1.EmailMailboxCredential{
		Name: credential.Name, Generation: credential.Generation, ConnectionRef: credential.ConnectionRef, ConnectionVersion: credential.ConnectionVersion,
		Kind: controlplanev1.EmailMailboxCredentialKind(controlplanev1.EmailMailboxCredentialKind_value["EMAIL_MAILBOX_CREDENTIAL_KIND_"+credential.Kind]),
	}
}

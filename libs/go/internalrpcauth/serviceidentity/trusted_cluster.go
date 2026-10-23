package serviceidentity

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	// TrustedClusterProfile names the explicitly configured trusted-cluster
	// transport and must not be inferred from caller-provided metadata.
	// RU: TrustedClusterProfile называет явно настроенный trusted-cluster
	// транспорт; его нельзя выбирать по metadata вызывающей стороны.
	TrustedClusterProfile = transportprofile.TrustedCluster
	// ProfileMetadataKey carries the profile assertion from an allowed workload.
	// RU: ProfileMetadataKey передаёт утверждение профиля разрешённого workload.
	ProfileMetadataKey = "x-kodex-rpc-profile"
	// CallerMetadataKey carries the trusted workload's claimed caller identity;
	// the target still resolves an exact method binding and domain authority.
	// RU: CallerMetadataKey передаёт заявленную identity доверенного workload;
	// target всё равно проверяет точный метод и доменные полномочия.
	CallerMetadataKey = "x-kodex-trusted-caller"
)

// TrustedClusterAuthorizer admits only an explicitly selected trusted-cluster
// server profile with an exact workload/method binding; the caller assertion
// is not cryptographic identity, so network isolation and domain checks remain
// mandatory, and a header cannot switch an mTLS server to this profile.
// RU: TrustedClusterAuthorizer допускает лишь явно выбранный серверный
// trusted-cluster профиль с точным workload/method; утверждение caller не
// является криптографической identity, поэтому сетевая изоляция и доменные
// проверки обязательны, а заголовок не переключает mTLS-сервер в этот режим.
type TrustedClusterAuthorizer struct {
	policy *Authorizer
}

// RPCProfile reports the trusted profile pinned by server configuration,
// never a transport mode selected by an incoming request header.
// RU: RPCProfile сообщает доверенный профиль из серверной конфигурации,
// а не режим транспорта, выбранный заголовком запроса.
func (*TrustedClusterAuthorizer) RPCProfile() string { return TrustedClusterProfile }

// UnaryServerInterceptor checks the asserted trusted caller and exact method
// before passing the server-owned admission to a unary handler.
// RU: UnaryServerInterceptor проверяет заявленного доверенного caller и
// точный метод до передачи серверного допуска unary-обработчику.
func (authorizer *TrustedClusterAuthorizer) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return unaryAdmissionInterceptor(authorizer.Admit)
}

// StreamServerInterceptor checks admission before the first stream message;
// actor and tenant are resolved by the target domain, not from payload.
// RU: StreamServerInterceptor проверяет допуск до первого сообщения потока;
// actor и tenant разрешает домен-получатель, а не payload.
func (authorizer *TrustedClusterAuthorizer) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		admission, err := authorizer.Admit(stream.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		return handler(server, &trustedAdmissionStream{ServerStream: stream, admission: admission})
	}
}

// trustedAdmissionStream exposes trusted-cluster admission to a stream handler
// without modifying the underlying gRPC message transport.
// RU: trustedAdmissionStream передаёт допуск trusted-cluster обработчику
// потока, не меняя нижележащую передачу сообщений gRPC.
type trustedAdmissionStream struct {
	grpc.ServerStream
	admission Admission
}

// Context places the server-issued admission in the stream context under a
// private key instead of trusting a caller-controlled metadata field.
// RU: Context помещает серверный допуск в контекст потока под закрытым ключом,
// а не доверяет полю metadata, доступному вызывающей стороне.
func (stream *trustedAdmissionStream) Context() context.Context {
	return context.WithValue(stream.ServerStream.Context(), admissionKey{}, stream.admission)
}

// TrustedClusterFromPolicy enables the profile only when explicitly selected,
// then validates the complete target-owned policy before constructing admission.
// RU: TrustedClusterFromPolicy включает профиль только при явном выборе и
// проверяет полную политику target перед созданием допуска.
func TrustedClusterFromPolicy(profile string, raw []byte, target string) (*TrustedClusterAuthorizer, error) {
	if profile != TrustedClusterProfile {
		return nil, errors.New("trusted cluster profile must be explicitly configured")
	}
	policy, _, err := FromPolicy(raw, target, CertificateLifetimeBoundary{})
	if err != nil {
		return nil, err
	}
	return &TrustedClusterAuthorizer{policy: policy}, nil
}

// NewTrustedCluster builds admission from already supplied exact bindings but
// still refuses an implicit or mismatched trusted-cluster profile.
// RU: NewTrustedCluster создаёт допуск из переданных точных bindings, но
// отклоняет неявный или несовпадающий trusted-cluster профиль.
func NewTrustedCluster(profile, target string, bindings []Binding) (*TrustedClusterAuthorizer, error) {
	if profile != TrustedClusterProfile {
		return nil, errors.New("trusted cluster profile must be explicitly configured")
	}
	policy, err := New(target, bindings, CertificateLifetimeBoundary{})
	if err != nil {
		return nil, err
	}
	return &TrustedClusterAuthorizer{policy: policy}, nil
}

// Admit validates one trusted metadata profile/caller pair, rejects mixed
// protected credentials and maps the exact caller/method to target-owned
// operation and actor mode; this is not a domain ownership decision.
// RU: Admit проверяет единственную пару profile/caller в trusted metadata,
// отклоняет смешанные protected credentials и сопоставляет точный caller/method
// серверным операции и actor mode; это не решение о владении доменным ресурсом.
func (authorizer *TrustedClusterAuthorizer) Admit(ctx context.Context, fullMethod string) (Admission, error) {
	if authorizer == nil || authorizer.policy == nil || ctx == nil {
		return Admission{}, status.Error(codes.Unauthenticated, "trusted cluster admission unavailable")
	}
	if err := ctx.Err(); err != nil {
		return Admission{}, status.FromContextError(err).Err()
	}
	md, _ := metadata.FromIncomingContext(ctx)
	profiles, callers := md.Get(ProfileMetadataKey), md.Get(CallerMetadataKey)
	if len(profiles) != 1 || profiles[0] != TrustedClusterProfile || len(callers) != 1 || len(md.Get("x-kodex-authorization")) != 0 {
		return Admission{}, status.Error(codes.Unauthenticated, "trusted cluster metadata rejected")
	}
	binding, ok := authorizer.policy.bindings[callers[0]+"\x00"+fullMethod]
	if !ok {
		return Admission{}, status.Error(codes.PermissionDenied, "trusted cluster RPC is not permitted")
	}
	// Нулевые поля сертификата намеренны: сертификат здесь не проверялся.
	return Admission{
		RPCProfile:     authorizer.RPCProfile(),
		Peer:           PeerIdentity{SPIFFEID: binding.CallerSPIFFEID},
		TargetSPIFFEID: authorizer.policy.target, FullMethod: fullMethod,
		OperationID: binding.OperationID, Permission: binding.Permission,
		ActorMode: binding.ActorMode, ProjectRequired: binding.ProjectRequired,
	}, nil
}

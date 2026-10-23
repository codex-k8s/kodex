package readback

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	// attestationProtectedType pins the protected JWS type for signed readback evidence; it does not identify the credential or restore-ACK protocol.
	// attestationProtectedType закрепляет защищённый тип JWS подписанного доказательства readback, не обозначая протокол credential или restore ACK.
	attestationProtectedType = "kodex-internal-rpc-readback-attestation+jws"
	// attestorAudience is the fixed audience of evidence submitted to the readback attestor, not authority derived from a caller-supplied address.
	// attestorAudience — фиксированная аудитория доказательств readback attestor, а не полномочие, полученное из адреса вызывающего.
	attestorAudience = "urn:kodex:internal-rpc-authority-readback-attestor"
	// attestationTTL bounds evidence expiry to thirty seconds after the server-issued challenge time; receipt expiry is validated independently.
	// attestationTTL ограничивает срок доказательства тридцатью секундами от серверного времени challenge; срок квитанции проверяется независимо.
	attestationTTL = 30 * time.Second
)

// Config binds one readback exchange to its workload, intent, credential, possession key and generations. TLS and the application interceptor remain separate required transport layers; carrying these values does not authenticate them by itself.
// Config связывает один обмен readback с workload, намерением, credential, ключом владения и поколениями. TLS и прикладной interceptor остаются отдельными обязательными слоями; наличие значений само не подтверждает их подлинность.
type Config struct {
	Address                 string
	TLS                     *tls.Config
	IntentID                string
	CredentialCompact       string
	CredentialJTI           string
	WorkloadID              string
	WorkloadSPIFFEID        string
	Role                    string
	WorkloadGeneration      uint64
	CredentialGeneration    uint64
	PossessionKeyGeneration uint64
	PossessionKey           internalrpcauth.ES256Key
	UnaryInterceptor        grpc.UnaryClientInterceptor
}

// Client signs readback evidence and exchanges it through one configured transport. Direct clients own that transport, while refresh wrappers reuse it and must close it only after their workers stop.
// Client подписывает доказательство readback и обменивается им через настроенный транспорт. Прямой клиент владеет транспортом; refresh-обёртки переиспользуют его и закрывают только после остановки своих workers.
type Client struct {
	config    Config
	transport *transport
}

// attestorAPI restricts the transport seam to challenge issuance and evidence submission; server-side ownership, signature and replay checks are not implemented by this interface.
// attestorAPI ограничивает транспортный интерфейс выдачей challenge и передачей доказательства; серверные проверки владения, подписи и повтора здесь не реализуются.
type attestorAPI interface {
	// IssueAttestationChallenge requests a challenge with the pinned intent and credential, reusing the same request identity only within one exchange's retries.
	// IssueAttestationChallenge запрашивает challenge с закреплёнными намерением и credential, повторяя идентичность запроса только при повторах одного обмена.
	IssueAttestationChallenge(
		context.Context,
		*internalrpcauthorityv1.IssueAttestationChallengeRequest,
		...grpc.CallOption,
	) (*internalrpcauthorityv1.IssueAttestationChallengeResponse, error)
	// AttestServedState submits the signed evidence and challenge binding and returns a receipt; the caller must validate the returned receipt fields before accepting it.
	// AttestServedState передаёт подписанное доказательство и привязку challenge и возвращает квитанцию; перед принятием вызывающий проверяет возвращённые поля.
	AttestServedState(
		context.Context,
		*internalrpcauthorityv1.AttestServedStateRequest,
		...grpc.CallOption,
	) (*internalrpcauthorityv1.AttestServedStateResponse, error)
}

// transport couples the narrow attestor API with its owning connection's close function, allowing refreshes and unit fakes to share one connection lifecycle.
// transport объединяет узкий API attestor с функцией закрытия принадлежащего ему соединения, позволяя refresh и юнит-заменам использовать один жизненный цикл.
type transport struct {
	api   attestorAPI
	close func() error
}

// transportFactory injects connection creation from an address, TLS profile and unary interceptor. Opening a transport does not prove remote readiness or successful attestation.
// transportFactory внедряет создание соединения по адресу, TLS-профилю и unary interceptor. Открытие транспорта не доказывает удалённую готовность или успешную аттестацию.
type transportFactory func(string, *tls.Config, grpc.UnaryClientInterceptor) (*transport, error)

// FileConfig pins four mounted-material paths plus transport and workload bindings. Material is reread on every refresh, and full client validation occurs after the private key is parsed.
// FileConfig закрепляет четыре пути смонтированного материала, транспорт и привязки workload. Материал перечитывается при каждом refresh, полная проверка клиента выполняется после разбора закрытого ключа.
type FileConfig struct {
	Address                  string
	TLS                      *tls.Config
	IntentIDFile             string
	CredentialCompactFile    string
	CredentialJTIFile        string
	PossessionPrivateJWKFile string
	WorkloadID               string
	WorkloadSPIFFEID         string
	Role                     string
	WorkloadGeneration       uint64
	CredentialGeneration     uint64
	PossessionKeyGeneration  uint64
	UnaryInterceptor         grpc.UnaryClientInterceptor
}

// FileAttestor refreshes credentials from bounded mounted-file reads while retaining one transport. It does not own the worker that schedules refreshes.
// FileAttestor обновляет credentials ограниченным чтением смонтированных файлов, сохраняя один транспорт. Планирующим refresh worker он не владеет.
type FileAttestor struct {
	config    FileConfig
	transport *transport
}

// SecretConfig binds named credential and possession-key deliveries to one workload and three generations. The injected reader owns delivery access; these paths are not shell commands or authority grants.
// SecretConfig связывает именованные доставки credential и ключа владения с workload и тремя поколениями. Доступом к доставке владеет внедрённый reader; пути не являются shell-командами или выдачей полномочий.
type SecretConfig struct {
	Address                 string
	TLS                     *tls.Config
	CredentialPath          string
	PossessionPath          string
	Delivery                SecretReader
	WorkloadID              string
	WorkloadSPIFFEID        string
	Role                    string
	WorkloadGeneration      uint64
	CredentialGeneration    uint64
	PossessionKeyGeneration uint64
	UnaryInterceptor        grpc.UnaryClientInterceptor
}

// SecretReader supplies versioned material through the delivery boundary without exposing a Kubernetes client here. Its implementation owns transport and version provenance.
// SecretReader предоставляет версионированный материал через границу доставки без Kubernetes-клиента здесь. Транспортом и происхождением версии владеет реализация.
type SecretReader interface {
	// ReadVersioned returns material, a distinct presence flag and an error for one exact delivery path; absence must not be interpreted as usable empty credentials.
	// ReadVersioned возвращает материал, отдельный признак присутствия и ошибку для точного пути доставки; отсутствие нельзя трактовать как пригодные пустые credentials.
	ReadVersioned(context.Context, string) (repository.SecretMaterial, bool, error)
}

// SecretAttestor rereads the credential and possession material for each exchange while reusing an owned transport; it does not itself poll Kubernetes or schedule refreshes.
// SecretAttestor перечитывает credential и материал владения для каждого обмена, переиспользуя свой транспорт; опрос Kubernetes и планирование refresh здесь не выполняются.
type SecretAttestor struct {
	config    SecretConfig
	transport *transport
}

// NewSecretAttestor constructs the Secret-backed wrapper using the production gRPC factory. It opens one transport but neither reads material nor performs the attestation yet.
// NewSecretAttestor создаёт Secret-обёртку с production-фабрикой gRPC. Один транспорт открывается, но материал ещё не читается и аттестация не выполняется.
func NewSecretAttestor(config SecretConfig) (*SecretAttestor, error) {
	return newSecretAttestor(config, newTransport)
}

// newSecretAttestor rejects missing delivery, workload, generation and transport configuration before calling the factory once. It checks TLS presence here; exact SNI and verification flags are checked by the client after material loading.
// newSecretAttestor отклоняет неполную конфигурацию доставки, workload, поколений и транспорта до однократного вызова фабрики. Здесь проверяется наличие TLS; точные SNI и флаги проверки проверяет клиент после загрузки материала.
func newSecretAttestor(config SecretConfig, factory transportFactory) (*SecretAttestor, error) {
	if config.Address == "" ||
		config.TLS == nil ||
		config.CredentialPath == "" ||
		config.PossessionPath == "" ||
		config.Delivery == nil ||
		config.WorkloadID == "" ||
		config.WorkloadSPIFFEID == "" ||
		config.Role == "" ||
		config.WorkloadGeneration == 0 ||
		config.CredentialGeneration == 0 ||
		config.PossessionKeyGeneration == 0 ||
		config.UnaryInterceptor == nil {
		return nil, errors.New("invalid readback Secret client configuration")
	}
	opened, err := factory(config.Address, config.TLS, config.UnaryInterceptor)
	if err != nil {
		return nil, fmt.Errorf("open readback attestor transport: %w", err)
	}
	return &SecretAttestor{config: config, transport: opened}, nil
}

// Close delegates to the shared transport's close function without a once guard. The owner must first stop refresh work; closing an individual exchange would close the shared connection.
// Close передаёт закрытие общему транспорту без защиты once. Владелец сначала останавливает refresh; закрытие отдельного обмена закрыло бы общее соединение.
func (attestor *SecretAttestor) Close() error {
	return attestor.transport.close()
}

// Attest reads credential material then possession material, rejects errors or absence, parses the private JWK and validates a client over the existing transport before exchanging evidence. It consumes the named data keys without independently verifying delivery versions or the credential signature here.
// Attest читает сначала credential, затем материал владения, отклоняет ошибки или отсутствие, разбирает закрытый JWK и проверяет клиента на существующем транспорте перед обменом. Используются именованные ключи данных без независимой проверки версий доставки или подписи credential здесь.
func (attestor *SecretAttestor) Attest(
	ctx context.Context,
	state repository.SnapshotState,
) (repository.SnapshotAttestationReceipt, error) {
	credentialMaterial, found, err := attestor.config.Delivery.ReadVersioned(
		ctx,
		attestor.config.CredentialPath,
	)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, fmt.Errorf("read normal readback credential: %w", err)
	}
	if !found {
		return repository.SnapshotAttestationReceipt{}, errors.New("read normal readback credential from Kubernetes Secret")
	}
	possessionMaterial, found, err := attestor.config.Delivery.ReadVersioned(
		ctx,
		attestor.config.PossessionPath,
	)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, fmt.Errorf("read readback possession key: %w", err)
	}
	if !found {
		return repository.SnapshotAttestationReceipt{}, errors.New("read readback possession key from Kubernetes Secret")
	}
	key, err := internalrpcauth.ParsePrivateJWK(
		[]byte(possessionMaterial.Data["possession_private_jwk"]),
	)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, fmt.Errorf("parse readback possession private key: %w", err)
	}
	client, err := newClient(Config{
		Address: attestor.config.Address, TLS: attestor.config.TLS,
		IntentID:                credentialMaterial.Data["pinned_intent_id"],
		CredentialCompact:       credentialMaterial.Data["readback_credential_compact_jws"],
		CredentialJTI:           credentialMaterial.Data["readback_credential_jti"],
		WorkloadID:              attestor.config.WorkloadID,
		WorkloadSPIFFEID:        attestor.config.WorkloadSPIFFEID,
		Role:                    attestor.config.Role,
		WorkloadGeneration:      attestor.config.WorkloadGeneration,
		CredentialGeneration:    attestor.config.CredentialGeneration,
		PossessionKeyGeneration: attestor.config.PossessionKeyGeneration,
		PossessionKey:           key,
		UnaryInterceptor:        attestor.config.UnaryInterceptor,
	}, attestor.transport)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, err
	}
	return client.Attest(ctx, state)
}

// NewFileAttestor constructs a mounted-file wrapper with one production gRPC transport. Credential reads and the attestation exchange are deferred until Attest.
// NewFileAttestor создаёт обёртку смонтированных файлов с одним production-транспортом gRPC. Чтение credentials и обмен аттестацией отложены до Attest.
func NewFileAttestor(config FileConfig) (*FileAttestor, error) {
	return newFileAttestor(config, newTransport)
}

// newFileAttestor separately rejects missing material paths and incomplete transport/workload bindings, then opens one transport through the injected factory. File safety and complete TLS/client checks are deferred until material is loaded.
// newFileAttestor отдельно отклоняет отсутствие путей материала и неполные привязки транспорта или workload, затем открывает один транспорт внедрённой фабрикой. Безопасность файлов и полные проверки TLS/клиента выполняются при загрузке материала.
func newFileAttestor(config FileConfig, factory transportFactory) (*FileAttestor, error) {
	if config.IntentIDFile == "" ||
		config.CredentialCompactFile == "" ||
		config.CredentialJTIFile == "" ||
		config.PossessionPrivateJWKFile == "" {
		return nil, errors.New("readback material file boundary is invalid")
	}
	if config.Address == "" ||
		config.TLS == nil ||
		config.WorkloadID == "" ||
		config.WorkloadSPIFFEID == "" ||
		config.Role == "" ||
		config.WorkloadGeneration == 0 ||
		config.CredentialGeneration == 0 ||
		config.PossessionKeyGeneration == 0 ||
		config.UnaryInterceptor == nil {
		return nil, errors.New("invalid readback file client configuration")
	}
	opened, err := factory(config.Address, config.TLS, config.UnaryInterceptor)
	if err != nil {
		return nil, fmt.Errorf("open readback attestor transport: %w", err)
	}
	return &FileAttestor{config: config, transport: opened}, nil
}

// Close delegates to the shared connection without coordinating workers or guarding repeated closes; the lifecycle owner must stop refreshes first.
// Close закрывает общее соединение без координации workers и защиты от повторных закрытий; владелец жизненного цикла сначала останавливает refresh.
func (attestor *FileAttestor) Close() error {
	return attestor.transport.close()
}

// Attest rereads intent and JTI within 128-byte bounds, the credential within the JWS bound and the private JWK within sixty-four KiB. It parses the key and runs a validated exchange over the existing connection; it does not establish a new transport per refresh.
// Attest перечитывает намерение и JTI с пределами 128 байтов, credential с пределом JWS и закрытый JWK с пределом шестьдесят четыре КиБ. После разбора ключа выполняется проверенный обмен на существующем соединении; новый транспорт на каждый refresh не создаётся.
func (attestor *FileAttestor) Attest(
	ctx context.Context,
	state repository.SnapshotState,
) (repository.SnapshotAttestationReceipt, error) {
	intentID, err := readMountedValue(attestor.config.IntentIDFile, 128)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, fmt.Errorf("read pinned readback intent: %w", err)
	}
	credential, err := readMountedValue(
		attestor.config.CredentialCompactFile,
		internalrpcauth.MaxCompactJWSBytes,
	)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, fmt.Errorf("read normal readback credential: %w", err)
	}
	credentialJTI, err := readMountedValue(attestor.config.CredentialJTIFile, 128)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, fmt.Errorf("read normal readback credential jti: %w", err)
	}
	privateRaw, err := readMountedValue(attestor.config.PossessionPrivateJWKFile, 64<<10)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, fmt.Errorf("read readback possession private key: %w", err)
	}
	key, err := internalrpcauth.ParsePrivateJWK([]byte(privateRaw))
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, fmt.Errorf("parse readback possession private key: %w", err)
	}
	client, err := newClient(Config{
		Address: attestor.config.Address, TLS: attestor.config.TLS,
		IntentID: intentID, CredentialCompact: credential,
		CredentialJTI:           credentialJTI,
		WorkloadID:              attestor.config.WorkloadID,
		WorkloadSPIFFEID:        attestor.config.WorkloadSPIFFEID,
		Role:                    attestor.config.Role,
		WorkloadGeneration:      attestor.config.WorkloadGeneration,
		CredentialGeneration:    attestor.config.CredentialGeneration,
		PossessionKeyGeneration: attestor.config.PossessionKeyGeneration,
		PossessionKey:           key,
		UnaryInterceptor:        attestor.config.UnaryInterceptor,
	}, attestor.transport)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, err
	}
	return client.Attest(ctx, state)
}

// readMountedValue uses the secure bounded file reader, trims surrounding whitespace and rejects empty results with fixed diagnostics. It returns a string and does not explicitly erase the buffer or returned material.
// readMountedValue использует безопасное ограниченное чтение файла, обрезает окружающие пробелы и отклоняет пустой результат фиксированной диагностикой. Возвращается строка; буфер и возвращённый материал явно не стираются.
func readMountedValue(path string, maximum int) (string, error) {
	raw, err := securefile.Read(path, int64(maximum))
	if err != nil {
		return "", errors.New("mounted readback file is unsafe")
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", errors.New("mounted readback file is empty")
	}
	return value, nil
}

// New validates complete in-memory bindings before opening a standalone gRPC transport. Its returned client owns Close, but creation alone does not establish peer readiness.
// New проверяет полные привязки в памяти до открытия отдельного транспорта gRPC. Возвращённый клиент владеет Close, но создание само не подтверждает готовность удалённой стороны.
func New(config Config) (*Client, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	opened, err := newTransport(config.Address, config.TLS, config.UnaryInterceptor)
	if err != nil {
		return nil, fmt.Errorf("open readback attestor transport: %w", err)
	}
	return &Client{config: config, transport: opened}, nil
}

// newClient validates the full configuration and requires a nonnil existing API and close function, then wraps that transport without opening or closing it. Refresh wrappers retain connection ownership.
// newClient проверяет полную конфигурацию и наличие существующего API с функцией закрытия, затем оборачивает транспорт без открытия или закрытия. Владение соединением остаётся у refresh-обёрток.
func newClient(config Config, opened *transport) (*Client, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if opened == nil || opened.api == nil || opened.close == nil {
		return nil, errors.New("invalid readback client transport")
	}
	return &Client{config: config, transport: opened}, nil
}

// validateConfig requires named identity/credential bindings, nonzero generations, a private possession key, an interceptor and TLS with a server name and verification enabled. It does not verify credential signatures, UUID grammar or all TLS trust settings.
// validateConfig требует именованные привязки идентичности и credential, ненулевые поколения, закрытый ключ владения, interceptor и TLS с именем сервера и включённой проверкой. Подписи credential, грамматика UUID и все настройки доверия TLS здесь не проверяются.
func validateConfig(config Config) error {
	if config.Address == "" ||
		config.TLS == nil ||
		config.TLS.ServerName == "" ||
		config.TLS.InsecureSkipVerify ||
		config.IntentID == "" ||
		config.CredentialCompact == "" ||
		config.CredentialJTI == "" ||
		config.WorkloadID == "" ||
		config.WorkloadSPIFFEID == "" ||
		config.Role == "" ||
		config.WorkloadGeneration == 0 ||
		config.CredentialGeneration == 0 ||
		config.PossessionKeyGeneration == 0 ||
		config.PossessionKey.Private == nil ||
		config.UnaryInterceptor == nil {
		return errors.New("invalid readback client configuration")
	}
	return nil
}

// newTransport creates the gRPC client with cloned TLS settings and the supplied unary interceptor, then couples the generated API to connection Close. gRPC client creation is not a successful network handshake or readiness probe.
// newTransport создаёт клиент gRPC с копией настроек TLS и переданным unary interceptor, связывая generated API с Close соединения. Создание клиента gRPC не является успешным сетевым рукопожатием или проверкой готовности.
func newTransport(
	address string,
	tlsConfig *tls.Config,
	interceptor grpc.UnaryClientInterceptor,
) (*transport, error) {
	connection, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig.Clone())),
		grpc.WithUnaryInterceptor(interceptor),
	)
	if err != nil {
		return nil, err
	}
	return &transport{
		api:   internalrpcauthorityv1.NewAuthorityReadbackAttestorServiceClient(connection),
		close: connection.Close,
	}, nil
}

// Close delegates to this client's transport and does not wait for in-flight exchanges; callers must respect standalone versus shared refresh ownership.
// Close передаёт закрытие транспорту клиента без ожидания текущих обменов; вызывающий соблюдает различие владения отдельного клиента и общего refresh-транспорта.
func (client *Client) Close() error {
	return client.transport.close()
}

// Attest obtains a fresh random challenge request identity, checks challenge bindings, signs evidence using server-issued time and submits the same signed bytes on Unavailable-only retries. Each RPC has at most three attempts with 100/250 ms waits; cancellation during a wait returns the context error directly. It rejects incomplete or expired receipts but does not recompute their evidence digest or authenticate credentials locally.
// Attest создаёт свежую случайную идентичность запроса challenge, проверяет его привязки, подписывает доказательство по серверному времени и повторяет те же подписанные байты только при Unavailable. Каждый RPC имеет не более трёх попыток с ожиданием 100/250 мс; отмена во время ожидания возвращает ошибку контекста напрямую. Неполные или просроченные квитанции отклоняются, но их дайджест доказательства не пересчитывается и credentials локально не аутентифицируются.
func (client *Client) Attest(
	ctx context.Context,
	state repository.SnapshotState,
) (repository.SnapshotAttestationReceipt, error) {
	credentialDigestRaw := sha256.Sum256([]byte(client.config.CredentialCompact))
	credentialDigest := hex.EncodeToString(credentialDigestRaw[:])
	// Receipt короче credential, поэтому каждый новый цикл attestation обязан
	// получить независимый challenge. Один request ID переиспользуется только
	// внутри этого вызова для безопасного повтора неоднозначного ответа.
	challengeKey, err := newRequestUUID()
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, fmt.Errorf("create readback challenge request identifier: %w", err)
	}
	challengeRequest := &internalrpcauthorityv1.IssueAttestationChallengeRequest{
		PinnedIntentId:               client.config.IntentID,
		ReadbackCredentialCompactJws: client.config.CredentialCompact,
		IdempotencyKey:               challengeKey,
		CorrelationId:                challengeKey,
	}
	challenge, err := callReadbackRPC(ctx, "issue readback attestation challenge", func() (
		*internalrpcauthorityv1.IssueAttestationChallengeResponse, error,
	) {
		return client.transport.api.IssueAttestationChallenge(ctx, challengeRequest)
	})
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, err
	}
	if challenge.GetKind() !=
		internalrpcauthorityv1.ReadbackAttestationKind_READBACK_ATTESTATION_KIND_SNAPSHOT ||
		challenge.GetChallengeId() == "" ||
		challenge.GetChallengeJti() == "" ||
		challenge.GetChallengeNonce() == "" ||
		challenge.GetChallengeDigestSha256() == "" ||
		challenge.GetReadbackCredentialDigestSha256() != credentialDigest ||
		challenge.GetWorkloadGeneration() != client.config.WorkloadGeneration ||
		challenge.GetCredentialGeneration() != client.config.CredentialGeneration ||
		challenge.GetPossessionKeyGeneration() != client.config.PossessionKeyGeneration ||
		challenge.GetIssuedAt() == nil || challenge.GetIssuedAt().CheckValid() != nil ||
		challenge.GetExpiresAt() == nil || challenge.GetExpiresAt().CheckValid() != nil ||
		!challenge.GetExpiresAt().AsTime().After(challenge.GetIssuedAt().AsTime()) {
		return repository.SnapshotAttestationReceipt{}, errors.New("readback challenge binding rejected")
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(
		client.config.PossessionKey.PublicOnly(),
	)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, fmt.Errorf("fingerprint readback possession key: %w", err)
	}
	// Повтор после неоднозначного ответа обязан подписывать байт-в-байт то же
	// evidence. Server-issued challenge time исключает локальные timestamp из
	// idempotency digest нескольких replica одного workload.
	now := challenge.GetIssuedAt().AsTime().UTC().Truncate(time.Second)
	evidence := model.ReadbackAttestationClaims{
		Version: model.ContractVersion, Issuer: client.config.WorkloadSPIFFEID,
		Audience: attestorAudience, Subject: client.config.WorkloadID,
		JTI:     deterministicUUID("readback-evidence", challenge.GetChallengeId()),
		Purpose: "SNAPSHOT_READBACK", IntentID: client.config.IntentID,
		IntentKind:               "SNAPSHOT",
		IntentRevision:           challenge.GetPinnedIntentRevision(),
		WorkloadID:               client.config.WorkloadID,
		WorkloadSPIFFEID:         client.config.WorkloadSPIFFEID,
		Role:                     client.config.Role,
		WorkloadGeneration:       client.config.WorkloadGeneration,
		CredentialGeneration:     client.config.CredentialGeneration,
		ReadbackCredentialJTI:    client.config.CredentialJTI,
		ReadbackCredentialDigest: credentialDigest,
		PossessionKeyID:          client.config.PossessionKey.KeyID,
		PossessionKeyGeneration:  client.config.PossessionKeyGeneration,
		PossessionKeyThumbprint:  thumbprint,
		SourceRevision:           state.SourceRevision,
		ServedStateDigestSHA256:  state.SourceDigestSHA256,
		ChallengeID:              challenge.GetChallengeId(),
		ChallengeJTI:             challenge.GetChallengeJti(),
		ChallengeNonce:           challenge.GetChallengeNonce(),
		ChallengeDigestSHA256:    challenge.GetChallengeDigestSha256(),
		IssuedAt:                 now.Unix(), NotBefore: now.Unix(),
		ExpiresAt: now.Add(attestationTTL).Unix(),
	}
	compact, err := internalrpcauth.SignCanonicalJSON(
		evidence,
		client.config.PossessionKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  attestationProtectedType,
			KeyID: client.config.PossessionKey.KeyID,
		},
	)
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, fmt.Errorf("sign readback attestation evidence: %w", err)
	}
	attestationKey := deterministicUUID(
		"readback-attestation",
		challenge.GetChallengeId(),
		evidence.JTI,
	)
	request := &internalrpcauthorityv1.AttestServedStateRequest{
		PinnedIntentId:                   client.config.IntentID,
		ChallengeId:                      challenge.GetChallengeId(),
		ReadbackCredentialCompactJws:     client.config.CredentialCompact,
		ServedStateAttestationCompactJws: compact,
		IdempotencyKey:                   attestationKey,
		CorrelationId:                    attestationKey,
	}
	receipt, err := callReadbackRPC(ctx, "attest served authority state", func() (
		*internalrpcauthorityv1.AttestServedStateResponse, error,
	) {
		return client.transport.api.AttestServedState(ctx, request)
	})
	if err != nil {
		return repository.SnapshotAttestationReceipt{}, err
	}
	if receipt.GetKind() !=
		internalrpcauthorityv1.ReadbackAttestationKind_READBACK_ATTESTATION_KIND_SNAPSHOT ||
		receipt.GetAttestationReceiptId() == "" ||
		receipt.GetEvidenceDigestSha256() == "" ||
		receipt.GetVerifierGeneration() == 0 ||
		receipt.GetExpiresAt() == nil || receipt.GetExpiresAt().CheckValid() != nil ||
		!receipt.GetExpiresAt().AsTime().After(time.Now()) {
		return repository.SnapshotAttestationReceipt{}, errors.New("readback attestation receipt binding rejected")
	}
	return repository.SnapshotAttestationReceipt{
		ReceiptID: receipt.GetAttestationReceiptId(),
		ExpiresAt: receipt.GetExpiresAt().AsTime().UTC(),
	}, nil
}

// deterministicUUID preserves the client protocol's SHA-256 identifier encoding with a NUL after every part and version-5/variant bits. This differs from joining parts without a trailing NUL and supplies neither randomness nor authority.
// deterministicUUID сохраняет клиентское кодирование идентификатора SHA-256 с NUL после каждой части и битами версии 5 и варианта. Это отличается от соединения без завершающего NUL и не даёт случайности или полномочий.
func deterministicUUID(parts ...string) string {
	return value.TerminatedAuthorityUUID(parts...)
}

// newRequestUUID fills sixteen bytes through crypto/rand and formats a version-4 RFC-variant UUID for a new exchange. It preserves the error-returning call shape; uniqueness is probabilistic, not a stored deduplication guarantee.
// newRequestUUID получает шестнадцать байтов через crypto/rand и форматирует UUID версии 4 с вариантом RFC для нового обмена. Сохраняется форма возврата ошибки; уникальность вероятностная, а не гарантия сохранённой дедупликации.
func newRequestUUID() (string, error) {
	return value.NewAuthorityUUID()
}

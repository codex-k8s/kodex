package restore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	domainrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

const (
	// maxKubernetesResponseBytes limits ConfigMap and TokenReview responses to two MiB; successful read paths consume one extra byte to detect overflow, while rejected-status bodies are only boundedly discarded.
	// maxKubernetesResponseBytes ограничивает ConfigMap и TokenReview responses двумя МиБ; успешные чтения берут дополнительный байт для обнаружения превышения, а тела отклонённых статусов лишь ограниченно отбрасываются.
	maxKubernetesResponseBytes = 2 << 20
	// stateDataKey identifies the canonical restore-coordination JSON entry inside the selected ConfigMap; unrelated ConfigMap data entries are retained during mutation.
	// stateDataKey обозначает запись canonical restore-coordination JSON выбранного ConfigMap; посторонние data entries сохраняются при изменении.
	stateDataKey = "state.json"
)

// Config supplies the HTTPS API endpoint/TLS name, CA and projected-token files, fixed coordination namespace/resource and bounded timeout. New validates URL shape but does not require the fixed default Kubernetes hostname used by other adapters.
// Config задаёт HTTPS API endpoint и TLS-имя, файлы CA и projected token, фиксированные coordination namespace и resource и ограниченный timeout. New проверяет форму URL, но не требует фиксированного default Kubernetes hostname других адаптеров.
type Config struct {
	Address       string
	TLSServerName string
	CAFile        string
	TokenFile     string
	Namespace     string
	ResourceName  string
	Timeout       time.Duration
}

// Store owns the HTTP client and exact ConfigMap URL for resourceVersion-CAS restore coordination and TokenReview. Domain methods expect trusted validated commands; this adapter is not an independent signature verifier or physical recovery executor.
// Store владеет HTTP-клиентом и точным URL ConfigMap для resourceVersion-CAS restore coordination и TokenReview. Доменные методы ожидают доверенные проверенные команды; адаптер не является независимым signature verifier и physical recovery executor.
type Store struct {
	config      Config
	client      *http.Client
	resourceURL string
}

// configMapEnvelope represents the ConfigMap API envelope, modeled object metadata and string data used for strict decoding and versioned writes; the canonical state has its own decoder.
// configMapEnvelope представляет API-оболочку ConfigMap, описанные object metadata и строковые данные для строгого декодирования и версионированной записи; canonical state разбирается отдельно.
type configMapEnvelope struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   objectMetadata    `json:"metadata"`
	Data       map[string]string `json:"data"`
}

// objectMetadata models the Kubernetes identity/version plus standard metadata fields preserved by read-modify-write. Raw owner references and managed fields remain opaque JSON; this struct alone does not authenticate ownership.
// objectMetadata описывает Kubernetes identity и version и стандартные metadata fields, сохраняемые read-modify-write. Raw owner references и managed fields остаются непрозрачным JSON; структура сама не аутентифицирует владение.
type objectMetadata struct {
	Name                       string            `json:"name"`
	GenerateName               string            `json:"generateName,omitempty"`
	Namespace                  string            `json:"namespace"`
	UID                        string            `json:"uid,omitempty"`
	ResourceVersion            string            `json:"resourceVersion,omitempty"`
	Generation                 int64             `json:"generation,omitempty"`
	CreationTimestamp          string            `json:"creationTimestamp,omitempty"`
	DeletionTimestamp          *string           `json:"deletionTimestamp,omitempty"`
	DeletionGracePeriodSeconds *int64            `json:"deletionGracePeriodSeconds,omitempty"`
	Labels                     map[string]string `json:"labels,omitempty"`
	Annotations                map[string]string `json:"annotations,omitempty"`
	OwnerReferences            []json.RawMessage `json:"ownerReferences,omitempty"`
	Finalizers                 []string          `json:"finalizers,omitempty"`
	ManagedFields              []json.RawMessage `json:"managedFields,omitempty"`
}

// tokenReviewRequest wraps a supplied operator token and exact audience in the authentication.k8s.io TokenReview create payload; it is sent only by the trusted store client.
// tokenReviewRequest оборачивает переданный operator token и точную audience в authentication.k8s.io TokenReview create payload, отправляемый лишь доверенным store client.
type tokenReviewRequest struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Spec       tokenReviewSpec `json:"spec"`
}

// tokenReviewSpec carries the operator credential being reviewed and its requested audience, distinct from the controller bearer authenticating the HTTP request.
// tokenReviewSpec передаёт проверяемый operator credential и запрошенную audience отдельно от controller bearer, аутентифицирующего HTTP-запрос.
type tokenReviewSpec struct {
	Token     string   `json:"token"`
	Audiences []string `json:"audiences"`
}

// tokenReviewResponse models the TokenReview version/kind, opaque metadata and returned review status; VerifyOperatorCredential validates the authenticated response's exact bindings.
// tokenReviewResponse описывает version и kind TokenReview, непрозрачные metadata и возвращённый review status; VerifyOperatorCredential проверяет точные привязки аутентифицированного ответа.
type tokenReviewResponse struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   json.RawMessage   `json:"metadata"`
	Status     tokenReviewStatus `json:"status"`
}

// tokenReviewStatus carries authentication, audiences, service-account identity, Pod-bound extras and provider error. Groups are decoded but not used to grant operator permission.
// tokenReviewStatus передаёт authentication, audiences, service-account identity, Pod-bound extras и provider error. Groups разбираются, но не используются для выдачи operator permission.
type tokenReviewStatus struct {
	Authenticated bool     `json:"authenticated"`
	Audiences     []string `json:"audiences"`
	User          struct {
		Username string              `json:"username"`
		UID      string              `json:"uid"`
		Groups   []string            `json:"groups"`
		Extra    map[string][]string `json:"extra"`
	} `json:"user"`
	Error string `json:"error"`
}

// New validates an HTTPS base URL without path/query/fragment, nonempty TLS name, fixed namespace/ConfigMap and bounded timeout, loads CA and configures TLS 1.3 and redirect rejection. It constructs a client without reading a token or contacting Kubernetes.
// New проверяет HTTPS base URL без path, query и fragment, непустое TLS-имя, фиксированные namespace и ConfigMap и ограниченный timeout, загружает CA и задаёт TLS 1.3 и запрет redirect. Клиент создаётся без чтения токена и обращения к Kubernetes.
func New(config Config) (*Store, error) {
	address, err := url.Parse(config.Address)
	if err != nil ||
		address.Scheme != "https" ||
		address.Host == "" ||
		address.Path != "" ||
		address.RawQuery != "" ||
		address.Fragment != "" ||
		config.TLSServerName == "" ||
		config.Namespace != "kodex-system" ||
		config.ResourceName != "internal-rpc-authority-restore-coordination" ||
		config.Timeout < time.Second ||
		config.Timeout > 10*time.Second {
		return nil, errors.New("invalid restore coordination Kubernetes configuration")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, errors.New("read Kubernetes API CA")
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("kubernetes API CA is invalid")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    rootCAs,
			ServerName: config.TLSServerName,
		},
		ForceAttemptHTTP2: true,
	}
	return &Store{
		config: config,
		client: &http.Client{
			Transport: transport,
			Timeout:   config.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("kubernetes API redirect is forbidden")
			},
		},
		resourceURL: config.Address +
			"/api/v1/namespaces/" + url.PathEscape(config.Namespace) +
			"/configmaps/" + url.PathEscape(config.ResourceName),
	}, nil
}

// Close closes idle Kubernetes HTTP connections without deleting coordination state, reversing committed mutations or cancelling caller-owned active requests.
// Close закрывает простаивающие Kubernetes HTTP-соединения без удаления coordination state, отката сохранённых изменений и отмены активных запросов вызывающего.
func (store *Store) Close() {
	store.client.CloseIdleConnections()
}

// Prepare returns matching stored idempotent state or begins QUIESCING from OPEN or a matured COMPLETED state, advancing epoch/anchor and resetting per-target records while retaining operator authorizations. The callback runs under bounded ConfigMap CAS; input authenticity belongs to the domain caller.
// Prepare возвращает совпавшее сохранённое идемпотентное состояние либо начинает QUIESCING из OPEN или созревшего COMPLETED, продвигая epoch и anchor и сбрасывая записи целей при сохранении operator authorizations. Callback работает через ограниченный ConfigMap CAS; подлинность ввода обеспечивает доменный caller.
func (store *Store) Prepare(
	ctx context.Context,
	command model.PrepareRestoreCommand,
) (model.RestoreState, error) {
	var result model.RestoreState
	err := store.mutate(ctx, func(current model.RestoreState) (model.RestoreState, error) {
		if current.Version != 0 {
			if current.PrepareIdempotencyKey == command.IdempotencyKey &&
				current.PrepareSemanticDigest == command.SemanticDigest {
				result = current
				return current, nil
			}
			if current.Phase != "OPEN" &&
				(current.Phase != "COMPLETED" ||
					current.SafeWindowNotBefore == 0 ||
					command.Now.Unix() < current.SafeWindowNotBefore) {
				return model.RestoreState{}, domainrepository.ErrIdempotencyConflict
			}
		}
		restoreEpoch := current.RestoreEpoch + 1
		anchorRevision := current.AnchorRevision + 1
		if restoreEpoch == 0 {
			restoreEpoch = 1
		}
		if anchorRevision == 0 {
			anchorRevision = 1
		}
		next := model.RestoreState{
			Version:                model.ContractVersion,
			RestoreID:              command.RestoreID,
			DatabaseClusterID:      command.DatabaseClusterID,
			BackupManifestDigest:   command.BackupManifestDigest,
			RecoveryTargetUnix:     command.RecoveryTarget.Unix(),
			Phase:                  "QUIESCING",
			RestoreEpoch:           restoreEpoch,
			CoordinationRevision:   current.CoordinationRevision + 1,
			ControllerGeneration:   command.ControllerGeneration,
			WorkloadSetRevision:    command.WorkloadSetRevision,
			AnchorRevision:         anchorRevision,
			EvidenceDigest:         command.SemanticDigest,
			PrepareIdempotencyKey:  command.IdempotencyKey,
			PrepareSemanticDigest:  command.SemanticDigest,
			ExpectedTargets:        command.ExpectedTargets,
			Issuances:              make(map[string]model.RestoreIssuanceRecord),
			Deliveries:             make(map[string]model.RestoreDeliveryRecord),
			Directives:             make(map[string]model.RestoreDirectiveRecord),
			ACKs:                   make(map[string]model.RestoreACKRecord),
			OperatorAuthorizations: current.OperatorAuthorizations,
			UpdatedAt:              command.Now.Unix(),
		}
		result = next
		return next, nil
	})
	return result, err
}

// Load reads the exact ConfigMap and returns zero state when its state entry is blank; otherwise it requires canonical JSON and the minimum restore-state fields, not full domain eligibility or signature verification.
// Load читает точный ConfigMap и возвращает нулевой state при пустой записи состояния; иначе требует canonical JSON и минимальные restore-state fields, а не полную доменную eligibility и signature verification.
func (store *Store) Load(ctx context.Context) (model.RestoreState, error) {
	envelope, err := store.read(ctx)
	if err != nil {
		return model.RestoreState{}, err
	}
	raw := envelope.Data[stateDataKey]
	if strings.TrimSpace(raw) == "" {
		return model.RestoreState{}, nil
	}
	return decodeState(raw)
}

// VerifyOperatorCredential posts the supplied operator token to TokenReview using a separately loaded controller bearer, requires exact audience/service-account and Pod-bound extras, and returns only identity plus token SHA-256. It does not reserve command authority; AuthorizeOperator performs that later.
// VerifyOperatorCredential отправляет переданный operator token в TokenReview с отдельно загруженным controller bearer, требует точные audience, service-account и Pod-bound extras и возвращает лишь identity и SHA-256 токена. Полномочия команды здесь не резервируются — позднее это делает AuthorizeOperator.
func (store *Store) VerifyOperatorCredential(
	ctx context.Context,
	token string,
	audience string,
) (model.RestoreOperatorCredential, error) {
	const (
		// expectedAudience pins the sole application audience accepted from TokenReview.
		// expectedAudience закрепляет единственную прикладную аудиторию результата TokenReview.
		expectedAudience = "urn:kodex:internal-rpc-authority-restore-controller"
		// expectedSubject fixes the operator ServiceAccount subject, independently of a payload-selected identity.
		// expectedSubject фиксирует subject operator ServiceAccount независимо от выбранной payload идентичности.
		expectedSubject = "system:serviceaccount:kodex-system:internal-rpc-authority-restore-operator"
		// podNameExtraKey selects the reviewed Pod-name binding that must match the operator workload prefix.
		// podNameExtraKey выбирает проверенную привязку имени Pod для совпадения с префиксом operator workload.
		podNameExtraKey = "authentication.kubernetes.io/pod-name"
		// podUIDExtraKey selects the required nonempty reviewed Pod UID without deriving it from request fields.
		// podUIDExtraKey выбирает обязательный непустой проверенный Pod UID без вывода из request fields.
		podUIDExtraKey = "authentication.kubernetes.io/pod-uid"
	)
	if token == "" || len(token) > 16<<10 || audience != expectedAudience {
		return model.RestoreOperatorCredential{}, errors.New(
			"restore operator application credential is invalid",
		)
	}
	body, err := json.Marshal(tokenReviewRequest{
		APIVersion: "authentication.k8s.io/v1",
		Kind:       "TokenReview",
		Spec: tokenReviewSpec{
			Token: token, Audiences: []string{audience},
		},
	})
	if err != nil {
		return model.RestoreOperatorCredential{}, errors.New(
			"encode restore operator TokenReview",
		)
	}
	request, err := store.apiRequest(
		ctx,
		http.MethodPost,
		store.config.Address+"/apis/authentication.k8s.io/v1/tokenreviews",
		body,
	)
	if err != nil {
		return model.RestoreOperatorCredential{}, err
	}
	response, err := store.client.Do(request)
	if err != nil {
		return model.RestoreOperatorCredential{}, errors.New(
			"perform restore operator TokenReview",
		)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(
		response.Body,
		maxKubernetesResponseBytes+1,
	))
	if err != nil ||
		response.StatusCode != http.StatusCreated ||
		len(raw) == 0 ||
		len(raw) > maxKubernetesResponseBytes {
		return model.RestoreOperatorCredential{}, errors.New(
			"restore operator TokenReview response rejected",
		)
	}
	var reviewed tokenReviewResponse
	if err := decodeStrictJSON(raw, &reviewed); err != nil ||
		reviewed.APIVersion != "authentication.k8s.io/v1" ||
		reviewed.Kind != "TokenReview" ||
		!reviewed.Status.Authenticated ||
		reviewed.Status.Error != "" ||
		reviewed.Status.User.Username != expectedSubject ||
		reviewed.Status.User.UID == "" ||
		len(reviewed.Status.User.Extra[podNameExtraKey]) != 1 ||
		!strings.HasPrefix(
			reviewed.Status.User.Extra[podNameExtraKey][0],
			"internal-rpc-authority-restore-operator-",
		) ||
		len(reviewed.Status.User.Extra[podUIDExtraKey]) != 1 ||
		reviewed.Status.User.Extra[podUIDExtraKey][0] == "" ||
		len(reviewed.Status.Audiences) != 1 ||
		reviewed.Status.Audiences[0] != audience {
		return model.RestoreOperatorCredential{}, errors.New(
			"restore operator TokenReview binding rejected",
		)
	}
	digest := sha256.Sum256([]byte(token))
	return model.RestoreOperatorCredential{
		Subject:           expectedSubject,
		Namespace:         "kodex-system",
		ServiceAccount:    "internal-rpc-authority-restore-operator",
		Audience:          audience,
		TokenDigestSHA256: hex.EncodeToString(digest[:]),
	}, nil
}

// EnsureIssuance requires the active QUIESCING restore, returns an existing target record unchanged or inserts one for an expected target. It does not compare a new proposal with an existing record or issue/sign a credential itself.
// EnsureIssuance требует активное QUIESCING-восстановление, возвращает прежнюю запись цели без изменения либо добавляет её для ожидаемой цели. Новое предложение не сравнивается с прежней записью и credential самостоятельно не выдаётся и не подписывается.
func (store *Store) EnsureIssuance(
	ctx context.Context,
	restoreID string,
	record model.RestoreIssuanceRecord,
) (model.RestoreIssuanceRecord, error) {
	var result model.RestoreIssuanceRecord
	err := store.mutate(ctx, func(current model.RestoreState) (model.RestoreState, error) {
		if current.RestoreID != restoreID || current.Phase != "QUIESCING" {
			return model.RestoreState{}, domainrepository.ErrNotFound
		}
		if existing, ok := current.Issuances[record.TargetID]; ok {
			result = existing
			return current, nil
		}
		if _, ok := current.ExpectedTargets[record.TargetID]; !ok {
			return model.RestoreState{}, domainrepository.ErrNotFound
		}
		current.Issuances[record.TargetID] = record
		current.UpdatedAt = time.Now().UTC().Unix()
		result = record
		return current, nil
	})
	return result, err
}

// RecordDelivery accepts an expected target in QUIESCING, persists a new delivery or requires an existing credential digest and receipt compact to match. It stores trusted caller evidence without verifying the receipt signature itself.
// RecordDelivery принимает ожидаемую цель в QUIESCING, сохраняет новую delivery либо требует совпадения прежних credential digest и receipt compact. Сохраняется доверенное caller evidence без самостоятельной проверки подписи квитанции.
func (store *Store) RecordDelivery(
	ctx context.Context,
	restoreID string,
	record model.RestoreDeliveryRecord,
) (model.RestoreState, error) {
	var result model.RestoreState
	err := store.mutate(ctx, func(current model.RestoreState) (model.RestoreState, error) {
		if current.RestoreID != restoreID || current.Phase != "QUIESCING" {
			return model.RestoreState{}, domainrepository.ErrNotFound
		}
		if existing, ok := current.Deliveries[record.TargetID]; ok {
			if existing.RoleCredentialDigestSHA256 != record.RoleCredentialDigestSHA256 ||
				existing.DeliveryReceiptCompactJWS != record.DeliveryReceiptCompactJWS {
				return model.RestoreState{}, domainrepository.ErrIdempotencyConflict
			}
			result = current
			return current, nil
		}
		if _, ok := current.ExpectedTargets[record.TargetID]; !ok {
			return model.RestoreState{}, domainrepository.ErrNotFound
		}
		current.Deliveries[record.TargetID] = record
		current.UpdatedAt = time.Now().UTC().Unix()
		result = current
		return current, nil
	})
	return result, err
}

// SaveDirective requires the active QUIESCING restore and a delivered target, reuses an unexpired stored directive or replaces it with the supplied record and wall-clock update time. Signature and exact target/source validation belong to the controller.
// SaveDirective требует активное QUIESCING-восстановление и доставленную цель, использует неистёкшую сохранённую директиву либо заменяет её переданной записью и настенным временем обновления. Подпись и точные target и source проверяет controller.
func (store *Store) SaveDirective(
	ctx context.Context,
	restoreID string,
	record model.RestoreDirectiveRecord,
) (model.RestoreDirectiveRecord, error) {
	var result model.RestoreDirectiveRecord
	err := store.mutate(ctx, func(current model.RestoreState) (model.RestoreState, error) {
		if current.RestoreID != restoreID || current.Phase != "QUIESCING" {
			return model.RestoreState{}, domainrepository.ErrNotFound
		}
		if _, delivered := current.Deliveries[record.TargetID]; !delivered {
			return model.RestoreState{}, domainrepository.ErrNotFound
		}
		if existing, ok := current.Directives[record.TargetID]; ok &&
			existing.ExpiresAt > time.Now().UTC().Unix() {
			result = existing
			return current, nil
		}
		current.Directives[record.TargetID] = record
		current.UpdatedAt = time.Now().UTC().Unix()
		result = record
		return current, nil
	})
	return result, err
}

// RecordACK CAS-records an expected target ACK in QUIESCING/PREPARED, returns an identical idempotent record or rejects conflicting reused keys/JTI, and moves to PREPARED when target/ACK counts match. It does not cryptographically validate ACK content or independently verify workload drain.
// RecordACK через CAS сохраняет ACK ожидаемой цели в QUIESCING или PREPARED, возвращает одинаковую идемпотентную запись либо отклоняет конфликт повторных keys и JTI и переводит в PREPARED при совпадении target и ACK counts. Криптографическая проверка ACK и независимая проверка workload drain не выполняются.
func (store *Store) RecordACK(
	ctx context.Context,
	restoreID string,
	record model.RestoreACKRecord,
) (model.RestoreState, model.RestoreACKRecord, error) {
	var result model.RestoreState
	var saved model.RestoreACKRecord
	err := store.mutate(ctx, func(current model.RestoreState) (model.RestoreState, error) {
		if current.RestoreID != restoreID ||
			(current.Phase != "QUIESCING" && current.Phase != "PREPARED") {
			return model.RestoreState{}, domainrepository.ErrNotFound
		}
		for _, existing := range current.ACKs {
			if existing.IdempotencyKey == record.IdempotencyKey ||
				existing.ACKJTI == record.ACKJTI {
				if existing.TargetID != record.TargetID ||
					existing.SemanticRequestDigest != record.SemanticRequestDigest ||
					existing.AcceptedACKDigest != record.AcceptedACKDigest {
					return model.RestoreState{}, domainrepository.ErrReplay
				}
				saved = existing
				result = current
				return current, nil
			}
		}
		if _, ok := current.ExpectedTargets[record.TargetID]; !ok {
			return model.RestoreState{}, domainrepository.ErrNotFound
		}
		record.ResultingPhase = "QUIESCING"
		current.ACKs[record.TargetID] = record
		if len(current.ACKs) == len(current.ExpectedTargets) {
			current.Phase = "PREPARED"
			current.CoordinationRevision++
			record.ResultingPhase = "PREPARED"
			current.ACKs[record.TargetID] = record
		}
		current.UpdatedAt = record.AcceptedAt
		saved = record
		result = current
		return current, nil
	})
	return result, saved, err
}

// AuthorizeOperator binds a token digest to exact subject/method/idempotency/semantic fields, permits the same binding again and rejects conflicting reuse. New records prune entries older than one hour relative to supplied time and cap the map at sixty-four; TokenReview and command eligibility occur before this adapter.
// AuthorizeOperator связывает token digest с точными subject, method, idempotency и semantic fields, допускает ту же привязку снова и отклоняет конфликт повторов. Для новых записей удаляются entries старше часа относительно переданного времени и карта ограничивается 64; TokenReview и command eligibility выполняются до адаптера.
func (store *Store) AuthorizeOperator(
	ctx context.Context,
	record model.RestoreOperatorAuthorizationRecord,
) error {
	return store.mutate(ctx, func(current model.RestoreState) (model.RestoreState, error) {
		if current.OperatorAuthorizations == nil {
			current.OperatorAuthorizations =
				make(map[string]model.RestoreOperatorAuthorizationRecord)
		}
		if existing, ok := current.OperatorAuthorizations[record.TokenDigestSHA256]; ok {
			if existing.Subject != record.Subject ||
				existing.FullMethod != record.FullMethod ||
				existing.IdempotencyKey != record.IdempotencyKey ||
				existing.SemanticDigestSHA256 != record.SemanticDigestSHA256 {
				return model.RestoreState{}, domainrepository.ErrReplay
			}
			return current, nil
		}
		for digest, existing := range current.OperatorAuthorizations {
			if record.AuthorizedAt-existing.AuthorizedAt > 3600 {
				delete(current.OperatorAuthorizations, digest)
			}
		}
		if len(current.OperatorAuthorizations) >= 64 {
			return model.RestoreState{}, domainrepository.ErrReplay
		}
		current.OperatorAuthorizations[record.TokenDigestSHA256] = record
		current.UpdatedAt = record.AuthorizedAt
		return current, nil
	})
}

// Complete returns matching idempotent state or requires exact PREPARED intent, ACK count and supplied evidence pins, then stores COMPLETED and a forty-second safe window no earlier than command time plus forty seconds. It does not itself verify evidence JWS or enforce the verifier's exact increment rule.
// Complete возвращает совпавшее идемпотентное состояние либо требует точный PREPARED intent, ACK count и переданные evidence pins, затем сохраняет COMPLETED и сорокасекундное safe window не ранее времени команды плюс сорок секунд. Evidence JWS и правило точного инкремента verifier самостоятельно не проверяются.
func (store *Store) Complete(
	ctx context.Context,
	command model.CompleteRestoreCommand,
) (model.RestoreState, error) {
	var result model.RestoreState
	err := store.mutate(ctx, func(current model.RestoreState) (model.RestoreState, error) {
		if current.CompleteIdempotencyKey == command.IdempotencyKey &&
			current.CompleteSemanticDigest == command.SemanticDigest {
			result = current
			return current, nil
		}
		if current.RestoreID != command.RestoreID ||
			current.DatabaseClusterID != command.DatabaseClusterID ||
			current.BackupManifestDigest != command.BackupManifestDigest ||
			current.RecoveryTargetUnix != command.RecoveryTarget.Unix() ||
			current.Phase != "PREPARED" ||
			len(current.ACKs) != len(current.ExpectedTargets) ||
			!validSHA256(command.EvidenceDigest) ||
			command.EvidenceAnchor <= current.AnchorRevision ||
			command.EvidenceRestoreEpoch != current.RestoreEpoch ||
			command.RestoredClusterUID == "" ||
			command.RestoredTimelineID == 0 ||
			command.RestoreCompletedAt.IsZero() ||
			command.RestoreCompletedAt.After(command.Now) {
			return model.RestoreState{}, domainrepository.ErrIdempotencyConflict
		}
		current.Phase = "COMPLETED"
		current.CoordinationRevision++
		current.AnchorRevision = command.EvidenceAnchor
		current.CompleteIdempotencyKey = command.IdempotencyKey
		current.CompleteSemanticDigest = command.SemanticDigest
		current.EvidenceDigest = command.EvidenceDigest
		current.EvidenceAnchorRevision = command.EvidenceAnchor
		current.RestoredClusterUID = command.RestoredClusterUID
		current.RestoredTimelineID = command.RestoredTimelineID
		safeWindow := command.RestoreCompletedAt.Add(40 * time.Second)
		if safeWindow.Before(command.Now.Add(40 * time.Second)) {
			safeWindow = command.Now.Add(40 * time.Second)
		}
		current.SafeWindowNotBefore = safeWindow.Unix()
		current.UpdatedAt = command.Now.Unix()
		result = current
		return current, nil
	})
	return result, err
}

// CoordinationReady verifies ConfigMap readability and, when nonblank, minimally valid canonical state. It does not perform a write probe, validate current restore phase or establish database fence readiness.
// CoordinationReady проверяет чтение ConfigMap и при непустой записи минимально допустимый canonical state. Write probe, допустимость текущей restore phase и готовность database fence не проверяются.
func (store *Store) CoordinationReady(ctx context.Context) error {
	envelope, err := store.read(ctx)
	if err != nil {
		return err
	}
	raw := envelope.Data[stateDataKey]
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	_, err = decodeState(raw)
	return err
}

// mutate performs at most five read/decode/callback/canonical-write attempts, retrying only resource-version conflict from a fresh read. Callbacks may run repeatedly; even a logical no-op writes the envelope, and a transport/readback failure can follow a persisted change.
// mutate выполняет до пяти попыток read, decode, callback и canonical write, повторяя лишь resource-version conflict со свежего чтения. Callback может выполняться повторно; даже логический no-op пишет envelope, а transport или readback failure возможен после сохранённого изменения.
func (store *Store) mutate(
	ctx context.Context,
	mutate func(model.RestoreState) (model.RestoreState, error),
) error {
	for attempt := 0; attempt < 5; attempt++ {
		envelope, err := store.read(ctx)
		if err != nil {
			return err
		}
		var current model.RestoreState
		if strings.TrimSpace(envelope.Data[stateDataKey]) != "" {
			current, err = decodeState(envelope.Data[stateDataKey])
			if err != nil {
				return err
			}
		}
		next, err := mutate(current)
		if err != nil {
			return err
		}
		raw, err := internalrpcauth.CanonicalJSON(next)
		if err != nil {
			return errors.New("encode restore coordination state")
		}
		envelope.APIVersion = "v1"
		envelope.Kind = "ConfigMap"
		envelope.Metadata.Name = store.config.ResourceName
		envelope.Metadata.Namespace = store.config.Namespace
		if envelope.Data == nil {
			envelope.Data = make(map[string]string)
		}
		envelope.Data[stateDataKey] = string(raw)
		updated, err := store.write(ctx, envelope)
		if errors.Is(err, errConflict) {
			continue
		}
		if err != nil {
			return err
		}
		if updated.Metadata.ResourceVersion == "" {
			return errors.New("kubernetes coordination CAS returned no resource version")
		}
		return nil
	}
	return errors.New("kubernetes coordination CAS retry budget exhausted")
}

// errConflict is the private sentinel for HTTP 409 ConfigMap writes, the sole condition retried by mutate; it is not a general transport retry signal.
// errConflict — закрытый sentinel HTTP 409 при записи ConfigMap и единственное условие повтора mutate, а не общий сигнал повтора transport failures.
var errConflict = errors.New("kubernetes coordination resource version conflict")

// read GETs the configured ConfigMap, bounds its body and strictly decodes known fields before requiring name/namespace and a nonempty resource version. It does not independently check API kind/version or canonical state content.
// read читает настроенный ConfigMap GET, ограничивает тело и строго декодирует известные поля до требования name, namespace и непустой resource version. API kind, version и canonical state content независимо не проверяются.
func (store *Store) read(ctx context.Context) (configMapEnvelope, error) {
	request, err := store.request(ctx, http.MethodGet, nil)
	if err != nil {
		return configMapEnvelope{}, err
	}
	response, err := store.client.Do(request)
	if err != nil {
		return configMapEnvelope{}, errors.New("read restore coordination ConfigMap")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxKubernetesResponseBytes))
		return configMapEnvelope{}, errors.New("restore coordination ConfigMap read rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxKubernetesResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxKubernetesResponseBytes {
		return configMapEnvelope{}, errors.New("restore coordination ConfigMap response is invalid")
	}
	var envelope configMapEnvelope
	if err := decodeStrictJSON(raw, &envelope); err != nil ||
		envelope.Metadata.Name != store.config.ResourceName ||
		envelope.Metadata.Namespace != store.config.Namespace ||
		envelope.Metadata.ResourceVersion == "" {
		return configMapEnvelope{}, errors.New("restore coordination ConfigMap binding rejected")
	}
	return envelope, nil
}

// write marshals and PUTs the supplied envelope, maps HTTP 409 to errConflict and strictly decodes a bounded HTTP 200 response. mutate checks the returned resource version; this method does not compare served state bytes with the requested state.
// write сериализует и отправляет PUT переданного envelope, отображает HTTP 409 в errConflict и строго декодирует ограниченный HTTP 200 response. Возвращённую resource version проверяет mutate; served state bytes здесь не сравниваются с запрошенными.
func (store *Store) write(
	ctx context.Context,
	envelope configMapEnvelope,
) (configMapEnvelope, error) {
	body, err := json.Marshal(envelope)
	if err != nil {
		return configMapEnvelope{}, errors.New("encode restore coordination ConfigMap")
	}
	request, err := store.request(ctx, http.MethodPut, body)
	if err != nil {
		return configMapEnvelope{}, err
	}
	response, err := store.client.Do(request)
	if err != nil {
		return configMapEnvelope{}, errors.New("write restore coordination ConfigMap")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusConflict {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxKubernetesResponseBytes))
		return configMapEnvelope{}, errConflict
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxKubernetesResponseBytes))
		return configMapEnvelope{}, errors.New("restore coordination ConfigMap update rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxKubernetesResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxKubernetesResponseBytes {
		return configMapEnvelope{}, errors.New("restore coordination ConfigMap update response is invalid")
	}
	var updated configMapEnvelope
	if err := decodeStrictJSON(raw, &updated); err != nil {
		return configMapEnvelope{}, errors.New("decode restore coordination ConfigMap update")
	}
	return updated, nil
}

// request constructs the context-bound HTTP request for the fixed coordination ConfigMap through apiRequest, without sending it or choosing another resource URL.
// request строит HTTP-запрос с контекстом фиксированного coordination ConfigMap через apiRequest без отправки и выбора другого resource URL.
func (store *Store) request(
	ctx context.Context,
	method string,
	body []byte,
) (*http.Request, error) {
	return store.apiRequest(ctx, method, store.resourceURL, body)
}

// apiRequest reloads the safe controller token and constructs a context-bound request for an adapter-owned URL, adding bearer and JSON content type for nonnil bodies. It does not perform the HTTP call or authenticate the operator token in a TokenReview body.
// apiRequest перечитывает безопасный controller token и строит запрос с контекстом принадлежащего адаптеру URL, добавляя bearer и JSON content type при ненулевом теле. HTTP call и аутентификация operator token внутри TokenReview body здесь не выполняются.
func (store *Store) apiRequest(
	ctx context.Context,
	method string,
	endpoint string,
	body []byte,
) (*http.Request, error) {
	token, err := readBoundToken(store.config.TokenFile)
	if err != nil {
		return nil, errors.New("read Kubernetes API workload token")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		method,
		endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, errors.New("construct Kubernetes coordination request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request, nil
}

// readBoundToken uses the projected-token secure-file boundary, trims whitespace and rejects empty or over-sixteen-KiB tokens, replacing filesystem errors with fixed credential-safe diagnostics.
// readBoundToken использует projected-token secure-file boundary, обрезает пробелы и отклоняет пустые токены и размер свыше шестнадцати КиБ, заменяя ошибки файловой системы фиксированной безопасной диагностикой.
func readBoundToken(path string) (string, error) {
	raw, err := securefile.ReadProjectedServiceAccountToken(path, 16<<10)
	if err != nil {
		return "", errors.New("workload token file is unsafe")
	}
	token := strings.TrimSpace(string(raw))
	if token == "" || len(token) > 16<<10 {
		return "", errors.New("workload token is invalid")
	}
	return token, nil
}

// decodeState requires canonical JSON and minimum contract/identity/revision/evidence/target presence, returning zero state on rejection. It does not validate every phase transition, digest grammar, target entry or cryptographic proof.
// decodeState требует canonical JSON и минимальное наличие contract, identity, revision, evidence и targets, возвращая нулевой state при отказе. Каждая phase transition, грамматика digest, target entry и cryptographic proof не проверяются.
func decodeState(raw string) (model.RestoreState, error) {
	var state model.RestoreState
	if err := internalrpcauth.DecodeCanonicalJSON([]byte(raw), &state); err != nil ||
		state.Version != model.ContractVersion ||
		state.RestoreID == "" ||
		state.RestoreEpoch == 0 ||
		state.CoordinationRevision == 0 ||
		state.AnchorRevision == 0 ||
		state.EvidenceDigest == "" ||
		len(state.ExpectedTargets) == 0 {
		return model.RestoreState{}, errors.New("restore coordination state is invalid")
	}
	return state, nil
}

// validSHA256 accepts a 64-character hex string decodable as thirty-two bytes, including uppercase hex. It checks shape rather than computing a digest or authenticating evidence.
// validSHA256 допускает 64-символьную hex-строку, декодируемую в 32 байта, включая верхний регистр. Проверяется форма, а не вычисление digest и аутентификация evidence.
func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// decodeStrictJSON rejects unknown fields and requires EOF after one JSON value; it preserves the existing wrapped trailing diagnostic. It has no duplicate-key/depth guard and may partially mutate the destination on decode failure.
// decodeStrictJSON отклоняет неизвестные поля и требует EOF после одного JSON-значения, сохраняя прежнюю обёрнутую trailing diagnostic. Duplicate-key и depth guard отсутствуют; destination может частично измениться при decode failure.
func decodeStrictJSON(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("unexpected trailing JSON: %w", err)
	}
	return nil
}

var _ domainrepository.RestoreCoordinationStore = (*Store)(nil)

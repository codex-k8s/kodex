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
	maxKubernetesResponseBytes = 2 << 20
	stateDataKey               = "state.json"
)

// Config задаёт точную TLS- и resource-конфигурацию хранилища.
type Config struct {
	Address       string
	TLSServerName string
	CAFile        string
	TokenFile     string
	Namespace     string
	ResourceName  string
	Timeout       time.Duration
}

// Store реализует версионированное CAS-хранилище координации.
type Store struct {
	config      Config
	client      *http.Client
	resourceURL string
}

type configMapEnvelope struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   objectMetadata    `json:"metadata"`
	Data       map[string]string `json:"data"`
}

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

type tokenReviewRequest struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Spec       tokenReviewSpec `json:"spec"`
}

type tokenReviewSpec struct {
	Token     string   `json:"token"`
	Audiences []string `json:"audiences"`
}

type tokenReviewResponse struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   json.RawMessage   `json:"metadata"`
	Status     tokenReviewStatus `json:"status"`
}

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

// New создаёт хранилище с закреплёнными namespace и resource name.
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

// Close закрывает простаивающие соединения клиента Kubernetes API.
func (store *Store) Close() {
	store.client.CloseIdleConnections()
}

// Prepare начинает новую координацию восстановления идемпотентно.
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

// Load читает и строго декодирует текущее состояние координации.
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

// VerifyOperatorCredential проверяет projected ServiceAccount token через
// Kubernetes TokenReview. Bearer value не возвращается и не логируется.
func (store *Store) VerifyOperatorCredential(
	ctx context.Context,
	token string,
	audience string,
) (model.RestoreOperatorCredential, error) {
	const (
		expectedAudience = "urn:kodex:internal-rpc-authority-restore-controller"
		expectedSubject  = "system:serviceaccount:kodex-system:internal-rpc-authority-restore-operator"
		podNameExtraKey  = "authentication.kubernetes.io/pod-name"
		podUIDExtraKey   = "authentication.kubernetes.io/pod-uid"
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

// EnsureIssuance сохраняет выдачу роли восстановления ровно один раз.
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

// RecordDelivery фиксирует подтверждённую доставку роли.
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

// SaveDirective сохраняет директиву остановки и дренирования.
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

// RecordACK атомарно фиксирует одноразовое подтверждение workload.
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

// AuthorizeOperator устойчиво связывает одноразовый token digest с exact
// RPC/idempotency/semantic request до изменения restore phase.
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

// Complete завершает подготовленный цикл восстановления идемпотентно.
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

// CoordinationReady проверяет доступность и корректность обслуживаемого состояния.
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

var errConflict = errors.New("kubernetes coordination resource version conflict")

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

func (store *Store) request(
	ctx context.Context,
	method string,
	body []byte,
) (*http.Request, error) {
	return store.apiRequest(ctx, method, store.resourceURL, body)
}

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

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

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

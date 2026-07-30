package restore

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	domainrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

const (
	maxKubernetesResponseBytes = 2 << 20
	stateDataKey               = "state.json"
)

type Config struct {
	Address       string
	TLSServerName string
	CAFile        string
	TokenFile     string
	Namespace     string
	ResourceName  string
	Timeout       time.Duration
}

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
	Name            string `json:"name"`
	Namespace       string `json:"namespace"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

func New(config Config) (*Store, error) {
	address, err := url.Parse(config.Address)
	if err != nil ||
		address.Scheme != "https" ||
		address.Host == "" ||
		address.Path != "" ||
		address.RawQuery != "" ||
		address.Fragment != "" ||
		config.TLSServerName == "" ||
		config.Namespace != "mattercodex-system" ||
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

func (store *Store) Close() {
	store.client.CloseIdleConnections()
}

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
			Version:               model.ContractVersion,
			RestoreID:             command.RestoreID,
			DatabaseClusterID:     command.DatabaseClusterID,
			BackupManifestDigest:  command.BackupManifestDigest,
			RecoveryTargetUnix:    command.RecoveryTarget.Unix(),
			Phase:                 "QUIESCING",
			RestoreEpoch:          restoreEpoch,
			CoordinationRevision:  current.CoordinationRevision + 1,
			AnchorRevision:        anchorRevision,
			EvidenceDigest:        command.SemanticDigest,
			PrepareIdempotencyKey: command.IdempotencyKey,
			PrepareSemanticDigest: command.SemanticDigest,
			ExpectedTargets:       command.ExpectedTargets,
			Issuances:             make(map[string]model.RestoreIssuanceRecord),
			Deliveries:            make(map[string]model.RestoreDeliveryRecord),
			Directives:            make(map[string]model.RestoreDirectiveRecord),
			ACKs:                  make(map[string]model.RestoreACKRecord),
			UpdatedAt:             command.Now.Unix(),
		}
		result = next
		return next, nil
	})
	return result, err
}

func (store *Store) Load(ctx context.Context) (model.RestoreState, error) {
	envelope, err := store.read(ctx)
	if err != nil {
		return model.RestoreState{}, err
	}
	return decodeState(envelope.Data[stateDataKey])
}

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
			len(current.ACKs) != len(current.ExpectedTargets) {
			return model.RestoreState{}, domainrepository.ErrIdempotencyConflict
		}
		current.Phase = "COMPLETED"
		current.CoordinationRevision++
		current.CompleteIdempotencyKey = command.IdempotencyKey
		current.CompleteSemanticDigest = command.SemanticDigest
		current.EvidenceDigest = command.SemanticDigest
		current.SafeWindowNotBefore = command.Now.Add(40 * time.Second).Unix()
		current.UpdatedAt = command.Now.Unix()
		result = current
		return current, nil
	})
	return result, err
}

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
	token, err := readBoundToken(store.config.TokenFile)
	if err != nil {
		return nil, errors.New("read Kubernetes API workload token")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		method,
		store.resourceURL,
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
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() ||
		info.Mode().Perm()&0o007 != 0 ||
		info.Size() <= 0 ||
		info.Size() > 16<<10 {
		return "", errors.New("workload token file is unsafe")
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
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

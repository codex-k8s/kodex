package pitr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

const (
	barmanPluginName = "barman-cloud.cloudnative-pg.io"
	cnpgAPIVersion   = "postgresql.cnpg.io/v1"
)

// ExecutorConfig задаёт immutable inputs фактического CloudNativePG PITR.
type ExecutorConfig struct {
	Address            string
	TLSServerName      string
	CAFile             string
	TokenFile          string
	Namespace          string
	EvidenceSecretName string
	PrivateJWKFile     string
	BarmanObjectName   string
	BarmanServerName   string
	PostgresImage      string
	StorageClass       string
	StorageSize        string
	Instances          uint32
	Timeout            time.Duration
}

// Executor создаёт отдельный CNPG cluster и публикует evidence только после
// readback healthy provider status с exact primary timeline.
type Executor struct {
	config      ExecutorConfig
	client      *http.Client
	privateKey  internalrpcauth.ES256Key
	clustersURL string
	evidenceURL string
	now         func() time.Time
}

type clusterEnvelope struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   objectMetadata  `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
	Status     clusterStatus   `json:"status"`
}

type objectMetadata struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	UID             string            `json:"uid"`
	ResourceVersion string            `json:"resourceVersion"`
	Generation      uint64            `json:"generation"`
	Annotations     map[string]string `json:"annotations"`
}

type clusterStatus struct {
	ObservedGeneration uint64                           `json:"observedGeneration"`
	Phase              string                           `json:"phase"`
	CurrentPrimary     string                           `json:"currentPrimary"`
	ReadyInstances     uint32                           `json:"readyInstances"`
	TimelineID         uint64                           `json:"timelineID"`
	InstancesReported  map[string]instanceReportedState `json:"instancesReportedState"`
	Conditions         []clusterCondition               `json:"conditions"`
}

type instanceReportedState struct {
	IsPrimary  bool   `json:"isPrimary"`
	TimelineID uint64 `json:"timeLineID"`
}

type clusterCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type desiredCluster struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   desiredMetadata `json:"metadata"`
	Spec       desiredSpec     `json:"spec"`
}

type desiredMetadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Annotations map[string]string `json:"annotations"`
	Labels      map[string]string `json:"labels"`
}

type desiredSpec struct {
	Instances        uint32            `json:"instances"`
	ImageName        string            `json:"imageName"`
	PrimaryStrategy  string            `json:"primaryUpdateStrategy"`
	Storage          desiredStorage    `json:"storage"`
	Bootstrap        desiredBootstrap  `json:"bootstrap"`
	ExternalClusters []externalCluster `json:"externalClusters"`
}

type desiredStorage struct {
	StorageClass string `json:"storageClass"`
	Size         string `json:"size"`
}

type desiredBootstrap struct {
	Recovery desiredRecovery `json:"recovery"`
}

type desiredRecovery struct {
	Source         string         `json:"source"`
	RecoveryTarget recoveryTarget `json:"recoveryTarget"`
}

type recoveryTarget struct {
	TargetTime string `json:"targetTime"`
	TargetTLI  string `json:"targetTLI"`
}

type externalCluster struct {
	Name   string         `json:"name"`
	Plugin externalPlugin `json:"plugin"`
}

type externalPlugin struct {
	Name       string            `json:"name"`
	Parameters map[string]string `json:"parameters"`
}

// NewExecutor создаёт отдельного owner фактического PITR.
func NewExecutor(config ExecutorConfig) (*Executor, error) {
	if config.Address != "https://kubernetes.default.svc:443" ||
		config.TLSServerName != "kubernetes.default.svc" ||
		config.Namespace != "mattercodex-system" ||
		config.EvidenceSecretName != "internal-rpc-authority-restore-evidence" ||
		config.BarmanObjectName == "" ||
		config.BarmanServerName != "internal-rpc-authority-primary" ||
		!strings.Contains(config.PostgresImage, "@sha256:") ||
		config.StorageClass == "" ||
		config.StorageSize == "" ||
		config.Instances != 3 ||
		config.Timeout <= 0 ||
		config.Timeout > 10*time.Second {
		return nil, errors.New("PITR executor configuration is invalid")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, errors.New("read Kubernetes API CA")
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("Kubernetes API CA is invalid")
	}
	privateRaw, err := os.ReadFile(config.PrivateJWKFile)
	if err != nil || len(privateRaw) == 0 || len(privateRaw) > 4096 {
		return nil, errors.New("read PITR evidence private key")
	}
	privateKey, err := internalrpcauth.ParsePrivateJWK(privateRaw)
	if err != nil {
		return nil, errors.New("parse PITR evidence private key")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    rootCAs,
			ServerName: config.TLSServerName,
		},
		ForceAttemptHTTP2: true,
	}
	base := config.Address + "/apis/postgresql.cnpg.io/v1/namespaces/" +
		url.PathEscape(config.Namespace) + "/clusters"
	return &Executor{
		config: config,
		client: &http.Client{
			Transport: transport,
			Timeout:   config.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("Kubernetes API redirect is forbidden")
			},
		},
		privateKey:  privateKey,
		clustersURL: base,
		evidenceURL: config.Address + "/api/v1/namespaces/" +
			url.PathEscape(config.Namespace) + "/secrets/" +
			url.PathEscape(config.EvidenceSecretName),
		now: time.Now,
	}, nil
}

// Close закрывает простаивающие Kubernetes API connections.
func (executor *Executor) Close() {
	executor.client.CloseIdleConnections()
}

// Execute создаёт или readback-проверяет exact PITR cluster и атомарно
// публикует signed evidence. Повтор после crash не создаёт новый cluster.
func (executor *Executor) Execute(
	ctx context.Context,
	state model.RestoreState,
) error {
	if state.Phase != "PREPARED" ||
		!validRestoreID(state.RestoreID) ||
		state.RestoreEpoch == 0 ||
		state.AnchorRevision == 0 ||
		state.ControllerGeneration == 0 ||
		state.WorkloadSetRevision == 0 ||
		len(state.ExpectedTargets) == 0 ||
		len(state.ACKs) != len(state.ExpectedTargets) {
		return errors.New("PITR executor requires exact PREPARED coordination")
	}
	cluster, desired, created, err := executor.ensureCluster(ctx, state)
	if err != nil {
		return err
	}
	if created {
		return errors.New("PITR cluster creation submitted")
	}
	timeline, err := validateHealthyCluster(cluster, desired, executor.config.Instances)
	if err != nil {
		return err
	}
	claims, err := executor.buildClaims(state, cluster, timeline)
	if err != nil {
		return err
	}
	return executor.publishEvidence(ctx, state, claims)
}

func (executor *Executor) ensureCluster(
	ctx context.Context,
	state model.RestoreState,
) (clusterEnvelope, desiredCluster, bool, error) {
	desired, err := executor.desiredCluster(state)
	if err != nil {
		return clusterEnvelope{}, desiredCluster{}, false, err
	}
	endpoint := executor.clustersURL + "/" + url.PathEscape(desired.Metadata.Name)
	response, raw, err := executor.do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return clusterEnvelope{}, desired, false, err
	}
	if response.StatusCode == http.StatusNotFound {
		body, marshalErr := json.Marshal(desired)
		if marshalErr != nil {
			return clusterEnvelope{}, desired, false, errors.New(
				"encode CloudNativePG PITR cluster",
			)
		}
		createResponse, _, createErr := executor.do(
			ctx,
			http.MethodPost,
			executor.clustersURL,
			body,
		)
		if createErr != nil {
			return clusterEnvelope{}, desired, false, createErr
		}
		if createResponse.StatusCode != http.StatusCreated &&
			createResponse.StatusCode != http.StatusConflict {
			return clusterEnvelope{}, desired, false, errors.New(
				"CloudNativePG PITR cluster creation rejected",
			)
		}
		return clusterEnvelope{}, desired, true, nil
	}
	if response.StatusCode != http.StatusOK {
		return clusterEnvelope{}, desired, false, errors.New(
			"CloudNativePG PITR cluster readback rejected",
		)
	}
	var cluster clusterEnvelope
	if err := json.Unmarshal(raw, &cluster); err != nil ||
		cluster.APIVersion != cnpgAPIVersion ||
		cluster.Kind != "Cluster" ||
		cluster.Metadata.Name != desired.Metadata.Name ||
		cluster.Metadata.Namespace != executor.config.Namespace ||
		cluster.Metadata.UID == "" ||
		!validResourceVersion(cluster.Metadata.ResourceVersion) ||
		cluster.Metadata.Generation == 0 {
		return clusterEnvelope{}, desired, false, errors.New(
			"CloudNativePG PITR cluster identity rejected",
		)
	}
	var servedSpec desiredSpec
	if err := json.Unmarshal(cluster.Spec, &servedSpec); err != nil {
		return clusterEnvelope{}, desired, false, errors.New(
			"decode served CloudNativePG PITR spec",
		)
	}
	servedSpecDigest, err := internalrpcauth.CanonicalJSONSHA256(servedSpec)
	if err != nil {
		return clusterEnvelope{}, desired, false, errors.New(
			"digest served CloudNativePG PITR spec",
		)
	}
	desiredSpecDigest, err := internalrpcauth.CanonicalJSONSHA256(desired.Spec)
	if err != nil || servedSpecDigest != desiredSpecDigest ||
		cluster.Metadata.Annotations["mattercodex.dev/restore-intent-digest-sha256"] !=
			desired.Metadata.Annotations["mattercodex.dev/restore-intent-digest-sha256"] {
		return clusterEnvelope{}, desired, false, errors.New(
			"CloudNativePG PITR cluster immutable intent mismatch",
		)
	}
	return cluster, desired, false, nil
}

func (executor *Executor) desiredCluster(
	state model.RestoreState,
) (desiredCluster, error) {
	if !validRestoreID(state.RestoreID) {
		return desiredCluster{}, errors.New("PITR restore ID is invalid")
	}
	restorePrefix := strings.ToLower(state.RestoreID[:8])
	name := fmt.Sprintf(
		"internal-rpc-authority-restore-%s-e%d",
		restorePrefix,
		state.RestoreEpoch,
	)
	intent := struct {
		RestoreID            string `json:"restore_id"`
		RestoreEpoch         uint64 `json:"restore_epoch"`
		BackupManifestDigest string `json:"backup_manifest_digest_sha256"`
		RecoveryTarget       int64  `json:"recovery_target_time"`
	}{
		RestoreID: state.RestoreID, RestoreEpoch: state.RestoreEpoch,
		BackupManifestDigest: state.BackupManifestDigest,
		RecoveryTarget:       state.RecoveryTargetUnix,
	}
	intentDigest, err := internalrpcauth.CanonicalJSONSHA256(intent)
	if err != nil {
		return desiredCluster{}, errors.New("digest immutable PITR intent")
	}
	return desiredCluster{
		APIVersion: cnpgAPIVersion,
		Kind:       "Cluster",
		Metadata: desiredMetadata{
			Name:      name,
			Namespace: executor.config.Namespace,
			Annotations: map[string]string{
				"mattercodex.dev/restore-id":                   state.RestoreID,
				"mattercodex.dev/restore-epoch":                strconv.FormatUint(state.RestoreEpoch, 10),
				"mattercodex.dev/restore-intent-digest-sha256": intentDigest,
			},
			Labels: map[string]string{
				"app.kubernetes.io/name":      "internal-rpc-authority-restore-pitr",
				"app.kubernetes.io/component": "restored-postgresql",
			},
		},
		Spec: desiredSpec{
			Instances:       executor.config.Instances,
			ImageName:       executor.config.PostgresImage,
			PrimaryStrategy: "unsupervised",
			Storage: desiredStorage{
				StorageClass: executor.config.StorageClass,
				Size:         executor.config.StorageSize,
			},
			Bootstrap: desiredBootstrap{Recovery: desiredRecovery{
				Source: "origin",
				RecoveryTarget: recoveryTarget{
					TargetTime: time.Unix(
						state.RecoveryTargetUnix,
						0,
					).UTC().Format(time.RFC3339),
					TargetTLI: "latest",
				},
			}},
			ExternalClusters: []externalCluster{{
				Name: "origin",
				Plugin: externalPlugin{
					Name: barmanPluginName,
					Parameters: map[string]string{
						"barmanObjectName": executor.config.BarmanObjectName,
						"serverName":       executor.config.BarmanServerName,
					},
				},
			}},
		},
	}, nil
}

func validateHealthyCluster(
	cluster clusterEnvelope,
	desired desiredCluster,
	instances uint32,
) (uint64, error) {
	if cluster.Status.ObservedGeneration != cluster.Metadata.Generation ||
		cluster.Status.Phase != "Cluster in healthy state" ||
		cluster.Status.ReadyInstances != instances ||
		cluster.Status.CurrentPrimary == "" ||
		cluster.Status.TimelineID == 0 ||
		!strings.HasPrefix(
			cluster.Status.CurrentPrimary,
			desired.Metadata.Name+"-",
		) {
		return 0, errors.New("CloudNativePG PITR cluster is not ready")
	}
	ready := false
	for _, condition := range cluster.Status.Conditions {
		if condition.Type == "Ready" &&
			condition.Status == "True" &&
			condition.Reason == "ClusterIsReady" {
			ready = true
			break
		}
	}
	primary, ok := cluster.Status.InstancesReported[cluster.Status.CurrentPrimary]
	if !ready || !ok || !primary.IsPrimary ||
		primary.TimelineID == 0 ||
		primary.TimelineID != cluster.Status.TimelineID {
		return 0, errors.New(
			"CloudNativePG PITR primary timeline readback rejected",
		)
	}
	return primary.TimelineID, nil
}

func (executor *Executor) buildClaims(
	state model.RestoreState,
	cluster clusterEnvelope,
	timeline uint64,
) (model.RestoreFenceEvidenceClaims, error) {
	expectedDigest, err := internalrpcauth.CanonicalJSONSHA256(
		state.ExpectedTargets,
	)
	if err != nil {
		return model.RestoreFenceEvidenceClaims{}, errors.New(
			"digest expected restore targets",
		)
	}
	ackDigest, err := internalrpcauth.CanonicalJSONSHA256(state.ACKs)
	if err != nil {
		return model.RestoreFenceEvidenceClaims{}, errors.New(
			"digest accepted restore ACK set",
		)
	}
	now := executor.now().UTC().Truncate(time.Second).Unix()
	return model.RestoreFenceEvidenceClaims{
		Version: model.ContractVersion, Issuer: evidenceIssuer,
		Audience: evidenceAudience, AnchorRevision: state.AnchorRevision + 1,
		RestoreEpoch: state.RestoreEpoch, Phase: "COMPLETED",
		DatabaseClusterID: state.DatabaseClusterID, RestoreID: state.RestoreID,
		BackupManifestDigestSHA256:            state.BackupManifestDigest,
		RecoveryTargetTime:                    state.RecoveryTargetUnix,
		ControllerSignerGeneration:            state.ControllerGeneration,
		WorkloadSetRevision:                   state.WorkloadSetRevision,
		ExpectedWorkloadRoleGenerationsSHA256: expectedDigest,
		QuiescenceACKSetSHA256:                ackDigest,
		ExpectedACKCount:                      uint32(len(state.ExpectedTargets)),
		AcceptedACKCount:                      uint32(len(state.ACKs)),
		SemanticTransition:                    "EXACT_INCREMENT_WITH_PREDECESSOR_DIGEST",
		Predecessor: model.RestoreEvidencePointer{
			Revision: state.AnchorRevision, DigestSHA256: state.EvidenceDigest,
		},
		IssuedAt: now, RestoreCompletedAt: now,
		RestoredClusterUID:             cluster.Metadata.UID,
		RestoredClusterResourceVersion: cluster.Metadata.ResourceVersion,
		RestoredPrimary:                cluster.Status.CurrentPrimary,
		RestoredTimelineID:             timeline,
		ProviderObservedGeneration:     cluster.Status.ObservedGeneration,
	}, nil
}

func (executor *Executor) publishEvidence(
	ctx context.Context,
	state model.RestoreState,
	claims model.RestoreFenceEvidenceClaims,
) error {
	response, raw, err := executor.do(
		ctx,
		http.MethodGet,
		executor.evidenceURL,
		nil,
	)
	if err != nil || response.StatusCode != http.StatusOK {
		return errors.New("read restore evidence Secret before CAS")
	}
	var current secretEnvelope
	if err := json.Unmarshal(raw, &current); err != nil ||
		current.Metadata.Name != executor.config.EvidenceSecretName ||
		current.Metadata.Namespace != executor.config.Namespace ||
		!validResourceVersion(current.Metadata.ResourceVersion) {
		return errors.New("restore evidence Secret CAS identity rejected")
	}
	if executor.existingEvidenceMatches(current, claims) {
		return nil
	}
	if current.Metadata.Annotations["mattercodex.dev/restore-anchor-revision"] !=
		strconv.FormatUint(state.AnchorRevision, 10) ||
		current.Metadata.Annotations["mattercodex.dev/restore-evidence-digest-sha256"] !=
			state.EvidenceDigest {
		return errors.New("restore evidence predecessor CAS rejected")
	}
	compact, err := internalrpcauth.SignCanonicalJSON(
		claims,
		executor.privateKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: evidenceType, KeyID: executor.privateKey.KeyID,
		},
	)
	if err != nil {
		return errors.New("sign independent PITR evidence")
	}
	digest := sha256.Sum256([]byte(compact))
	digestHex := hex.EncodeToString(digest[:])
	current.APIVersion = "v1"
	current.Kind = "Secret"
	current.Type = "Opaque"
	current.Metadata.Annotations = map[string]string{
		"mattercodex.dev/restore-anchor-revision":           strconv.FormatUint(claims.AnchorRevision, 10),
		"mattercodex.dev/restore-epoch":                     strconv.FormatUint(claims.RestoreEpoch, 10),
		"mattercodex.dev/restore-evidence-digest-sha256":    digestHex,
		"mattercodex.dev/restore-predecessor-revision":      strconv.FormatUint(claims.Predecessor.Revision, 10),
		"mattercodex.dev/restore-predecessor-digest-sha256": claims.Predecessor.DigestSHA256,
		"mattercodex.dev/restored-cluster-uid":              claims.RestoredClusterUID,
		"mattercodex.dev/restored-timeline-id":              strconv.FormatUint(claims.RestoredTimelineID, 10),
	}
	current.Data = map[string]string{
		evidenceDataKey: base64.StdEncoding.EncodeToString([]byte(compact)),
	}
	body, err := json.Marshal(current)
	if err != nil {
		return errors.New("encode restore evidence Secret CAS")
	}
	updateResponse, _, err := executor.do(
		ctx,
		http.MethodPut,
		executor.evidenceURL,
		body,
	)
	if err != nil || updateResponse.StatusCode != http.StatusOK {
		return errors.New("update restore evidence Secret CAS")
	}
	servedResponse, servedRaw, err := executor.do(
		ctx,
		http.MethodGet,
		executor.evidenceURL,
		nil,
	)
	if err != nil || servedResponse.StatusCode != http.StatusOK {
		return errors.New("read back served restore evidence Secret")
	}
	var served secretEnvelope
	if err := json.Unmarshal(servedRaw, &served); err != nil ||
		!executor.existingEvidenceMatches(served, claims) {
		return errors.New("served restore evidence readback mismatch")
	}
	return nil
}

func (executor *Executor) existingEvidenceMatches(
	secret secretEnvelope,
	expected model.RestoreFenceEvidenceClaims,
) bool {
	encoded := secret.Data[evidenceDataKey]
	compact, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(compact) == 0 {
		return false
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		string(compact),
		executor.privateKey.PublicOnly(),
		internalrpcauth.ProtectedHeaderExpectation{
			Type: evidenceType, KeyID: executor.privateKey.KeyID,
		},
	)
	if err != nil {
		return false
	}
	var actual model.RestoreFenceEvidenceClaims
	if err := internalrpcauth.DecodeCanonicalJSON(
		verified.CanonicalPayload,
		&actual,
	); err != nil {
		return false
	}
	if !sameEvidenceIntent(actual, expected) {
		return false
	}
	digest := sha256.Sum256(compact)
	return verifyAnnotations(
		secret.Metadata.Annotations,
		actual,
		hex.EncodeToString(digest[:]),
	) == nil
}

func sameEvidenceIntent(
	actual model.RestoreFenceEvidenceClaims,
	expected model.RestoreFenceEvidenceClaims,
) bool {
	actual.IssuedAt = 0
	actual.RestoreCompletedAt = 0
	actual.RestoredClusterResourceVersion = ""
	expected.IssuedAt = 0
	expected.RestoreCompletedAt = 0
	expected.RestoredClusterResourceVersion = ""
	actualCanonical, err := internalrpcauth.CanonicalJSON(actual)
	if err != nil {
		return false
	}
	expectedCanonical, err := internalrpcauth.CanonicalJSON(expected)
	return err == nil && bytes.Equal(actualCanonical, expectedCanonical)
}

func (executor *Executor) do(
	ctx context.Context,
	method string,
	endpoint string,
	body []byte,
) (*http.Response, []byte, error) {
	token, err := readToken(executor.config.TokenFile)
	if err != nil {
		return nil, nil, errors.New("read Kubernetes API workload token")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		method,
		endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, nil, errors.New("construct Kubernetes PITR request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := executor.client.Do(request)
	if err != nil {
		return nil, nil, errors.New("perform Kubernetes PITR request")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
		return nil, nil, errors.New("Kubernetes PITR response rejected")
	}
	return response, raw, nil
}

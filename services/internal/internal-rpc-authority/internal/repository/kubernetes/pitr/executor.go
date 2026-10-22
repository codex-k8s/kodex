// Package pitr executes and verifies authority-owned PostgreSQL recovery through
// fixed Kubernetes resources and independently signed completion evidence.
// Пакет pitr выполняет и проверяет принадлежащее authority восстановление PostgreSQL
// через фиксированные Kubernetes-ресурсы и независимо подписанное completion evidence.
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

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

const (
	// barmanPluginName fixes the CloudNativePG Barman plugin identity for backup resolution and recovery manifests; it is not selected from untrusted restore payloads.
	// barmanPluginName фиксирует идентичность CloudNativePG Barman plugin для разрешения backup и recovery manifests без выбора из недоверенных restore payloads.
	barmanPluginName = "barman-cloud.cloudnative-pg.io"
	// cnpgAPIVersion pins the CloudNativePG resource API version expected in observed Backup/Cluster envelopes and generated recovery Cluster objects.
	// cnpgAPIVersion закрепляет версию CloudNativePG API в наблюдённых оболочках Backup и Cluster и создаваемых объектах recovery Cluster.
	cnpgAPIVersion = "postgresql.cnpg.io/v1"
)

// ExecutorConfig pins Kubernetes/TLS and evidence-key paths, selected backup/source/Barman settings, PostgreSQL image/storage, three instances and timeout. Constructor shape checks do not authenticate image provenance or validate storage quantities.
// ExecutorConfig закрепляет Kubernetes и TLS, пути evidence key, настройки выбранных backup, source и Barman, образ PostgreSQL, storage, три экземпляра и timeout. Проверки формы конструктора не аутентифицируют image provenance и не проверяют storage quantities.
type ExecutorConfig struct {
	Address            string
	TLSServerName      string
	CAFile             string
	TokenFile          string
	Namespace          string
	EvidenceSecretName string
	PrivateJWKFile     string
	BackupName         string
	SourceClusterName  string
	BarmanObjectName   string
	BarmanServerName   string
	PostgresImage      string
	StorageClass       string
	StorageSize        string
	Instances          uint32
	Timeout            time.Duration
}

// Executor owns the client, private evidence signer and manifest resolver used to create/read a deterministic recovery Cluster and publish completion evidence. External Cluster/Secret writes are separate effects, not one transaction with coordination state.
// Executor владеет клиентом, закрытым evidence signer и manifest resolver для создания и чтения детерминированного recovery Cluster и публикации completion evidence. Внешние записи Cluster и Secret — отдельные эффекты, а не одна транзакция с coordination state.
type Executor struct {
	config      ExecutorConfig
	client      *http.Client
	privateKey  internalrpcauth.ES256Key
	manifest    *ManifestResolver
	clustersURL string
	evidenceURL string
	now         func() time.Time
}

// clusterEnvelope captures observed Cluster identity, raw spec and modeled health status; ordinary JSON decoding allows additional provider fields.
// clusterEnvelope содержит наблюдённые identity Cluster, исходный spec и описанный health status; обычное JSON-декодирование допускает дополнительные provider fields.
type clusterEnvelope struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   objectMetadata  `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
	Status     clusterStatus   `json:"status"`
}

// objectMetadata carries modeled Kubernetes identity/version/generation and annotation pins shared by Backup and Cluster readback. Nonempty values alone do not authenticate their source.
// objectMetadata передаёт описанные Kubernetes identity, version, generation и annotation pins для readback Backup и Cluster. Одни непустые значения не аутентифицируют источник.
type objectMetadata struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	UID             string            `json:"uid"`
	ResourceVersion string            `json:"resourceVersion"`
	Generation      uint64            `json:"generation"`
	Annotations     map[string]string `json:"annotations"`
}

// clusterStatus models observed generation, aggregate readiness, primary/timeline and per-instance conditions used by validateHealthyCluster; it is provider-reported data, not a fresh SQL probe.
// clusterStatus описывает observed generation, суммарную readiness, primary, timeline и поэкземплярные conditions для validateHealthyCluster; это provider-reported data, а не свежая SQL-проверка.
type clusterStatus struct {
	ObservedGeneration uint64                           `json:"observedGeneration"`
	Phase              string                           `json:"phase"`
	CurrentPrimary     string                           `json:"currentPrimary"`
	ReadyInstances     uint32                           `json:"readyInstances"`
	TimelineID         uint64                           `json:"timelineID"`
	InstancesReported  map[string]instanceReportedState `json:"instancesReportedState"`
	Conditions         []clusterCondition               `json:"conditions"`
}

// instanceReportedState preserves the provider's primary flag and case-sensitive timeLineID field used to bind the chosen primary to the aggregate timeline.
// instanceReportedState сохраняет primary flag и регистрозависимое поле timeLineID provider для привязки выбранного primary к общему timeline.
type instanceReportedState struct {
	IsPrimary  bool   `json:"isPrimary"`
	TimelineID uint64 `json:"timeLineID"`
}

// clusterCondition carries one provider condition's type/status/reason; health requires the exact Ready/True/ClusterIsReady tuple rather than arbitrary truth-like text.
// clusterCondition передаёт type, status и reason одной provider condition; health требует точного набора Ready, True и ClusterIsReady, а не произвольного похожего текста.
type clusterCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// desiredCluster is the typed create payload for the deterministic recovery Cluster, combining fixed API kind, immutable intent annotations and the selected recovery spec.
// desiredCluster — типизированный create payload детерминированного recovery Cluster с фиксированным API kind, immutable intent annotations и выбранным recovery spec.
type desiredCluster struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   desiredMetadata `json:"metadata"`
	Spec       desiredSpec     `json:"spec"`
}

// desiredMetadata holds the deterministic recovery name/namespace, intent annotations and workload labels; provider-created UID/resourceVersion are not synthesized here.
// desiredMetadata содержит детерминированные recovery name и namespace, intent annotations и workload labels; назначаемые provider UID и resourceVersion здесь не выдумываются.
type desiredMetadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Annotations map[string]string `json:"annotations"`
	Labels      map[string]string `json:"labels"`
}

// desiredSpec contains the modeled instance/image/strategy/storage/bootstrap/external-cluster fields used for canonical intent comparison. Unmodeled provider spec fields are not part of this typed comparison.
// desiredSpec содержит описанные instance, image, strategy, storage, bootstrap и external-cluster fields для canonical intent comparison. Неописанные provider spec fields в типизированное сравнение не входят.
type desiredSpec struct {
	Instances        uint32            `json:"instances"`
	ImageName        string            `json:"imageName"`
	PrimaryStrategy  string            `json:"primaryUpdateStrategy"`
	Storage          desiredStorage    `json:"storage"`
	Bootstrap        desiredBootstrap  `json:"bootstrap"`
	ExternalClusters []externalCluster `json:"externalClusters"`
}

// desiredStorage copies the configured storage class and size strings into the recovery manifest without parsing quantities or provisioning storage itself.
// desiredStorage копирует настроенные строки storage class и size в recovery manifest без разбора quantities и самостоятельного выделения storage.
type desiredStorage struct {
	StorageClass string `json:"storageClass"`
	Size         string `json:"size"`
}

// desiredBootstrap wraps the recovery-only bootstrap specification rather than an initialization or import mode.
// desiredBootstrap оборачивает recovery-only bootstrap specification, а не режим initialization или import.
type desiredBootstrap struct {
	Recovery desiredRecovery `json:"recovery"`
}

// desiredRecovery binds the fixed origin external-cluster alias to the exact target time, source timeline and provider backup ID chosen for recovery.
// desiredRecovery связывает фиксированный alias origin внешнего cluster с точными target time, source timeline и provider backup ID восстановления.
type desiredRecovery struct {
	Source         string         `json:"source"`
	RecoveryTarget recoveryTarget `json:"recoveryTarget"`
}

// recoveryTarget serializes UTC RFC3339 target time, decimal timeline and provider backup ID; it is configuration for CNPG, not evidence recovery has completed.
// recoveryTarget сериализует UTC RFC3339 target time, десятичный timeline и provider backup ID; это конфигурация CNPG, а не evidence завершённого восстановления.
type recoveryTarget struct {
	TargetTime string `json:"targetTime"`
	TargetTLI  string `json:"targetTLI"`
	BackupID   string `json:"backupID"`
}

// externalCluster associates the fixed origin source alias with its Barman recovery plugin configuration inside the desired Cluster spec.
// externalCluster связывает фиксированный source alias origin с конфигурацией Barman recovery plugin в желаемом Cluster spec.
type externalCluster struct {
	Name   string         `json:"name"`
	Plugin externalPlugin `json:"plugin"`
}

// externalPlugin supplies the Barman plugin name and exact object/server parameter map used to select backup storage, without embedding storage credentials.
// externalPlugin задаёт имя Barman plugin и точную карту параметров object и server для выбора backup storage без включения storage credentials.
type externalPlugin struct {
	Name       string            `json:"name"`
	Parameters map[string]string `json:"parameters"`
}

// NewExecutor validates fixed Kubernetes/source/evidence bindings, three instances, timeout, nonempty storage and the image's @sha256: substring, loads CA/private JWK and creates a manifest resolver. It configures TLS 1.3 and redirect refusal without making API calls; full image syntax/trust and key delivery are external responsibilities.
// NewExecutor проверяет фиксированные Kubernetes, source и evidence bindings, три экземпляра, timeout, непустой storage и подстроку @sha256: образа, загружает CA и private JWK и создаёт manifest resolver. TLS 1.3 и запрет redirect задаются без API calls; полный синтаксис образа, доверие ему и доставку ключа обеспечивают внешние механизмы.
func NewExecutor(config ExecutorConfig) (*Executor, error) {
	if config.Address != "https://kubernetes.default.svc:443" ||
		config.TLSServerName != "kubernetes.default.svc" ||
		config.Namespace != "kodex-system" ||
		config.EvidenceSecretName != "internal-rpc-authority-restore-evidence" ||
		config.BackupName == "" ||
		config.SourceClusterName != "internal-rpc-authority-primary" ||
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
		return nil, errors.New("kubernetes API CA is invalid")
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
	manifest, err := NewManifestResolver(ManifestResolverConfig{
		Address: config.Address, TLSServerName: config.TLSServerName,
		CAFile: config.CAFile, TokenFile: config.TokenFile,
		Namespace: config.Namespace, BackupName: config.BackupName,
		SourceCluster:    config.SourceClusterName,
		BarmanObjectName: config.BarmanObjectName,
		BarmanServerName: config.BarmanServerName, Timeout: config.Timeout,
	})
	if err != nil {
		return nil, err
	}
	return &Executor{
		config: config,
		client: &http.Client{
			Transport: transport,
			Timeout:   config.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("kubernetes API redirect is forbidden")
			},
		},
		privateKey:  privateKey,
		manifest:    manifest,
		clustersURL: base,
		evidenceURL: config.Address + "/api/v1/namespaces/" +
			url.PathEscape(config.Namespace) + "/secrets/" +
			url.PathEscape(config.EvidenceSecretName),
		now: time.Now,
	}, nil
}

// Close closes both manifest-resolver and executor idle HTTP connections; it does not cancel a caller context, delete recovery Clusters or erase signed evidence.
// Close закрывает простаивающие HTTP-соединения manifest resolver и executor без отмены caller context, удаления recovery Clusters и стирания signed evidence.
func (executor *Executor) Close() {
	executor.manifest.Close()
	executor.client.CloseIdleConnections()
}

// Execute requires PREPARED state with matching target/ACK counts and manifest digest, ensures the deterministic Cluster, validates observed health, builds claims and publishes evidence. A submitted create returns a progress error for a later retry; the method neither waits in a loop nor advances coordination to COMPLETED itself.
// Execute требует PREPARED state с совпадающими target и ACK counts и manifest digest, обеспечивает детерминированный Cluster, проверяет наблюдённый health, строит claims и публикует evidence. Отправленный create возвращает progress error для последующего повтора; метод не ожидает в цикле и сам не переводит coordination в COMPLETED.
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
	manifest, err := executor.manifest.Resolve(
		ctx,
		time.Unix(state.RecoveryTargetUnix, 0),
	)
	if err != nil {
		return err
	}
	if manifest.DigestSHA256 != state.BackupManifestDigest {
		return errors.New("authoritative CNPG backup manifest digest mismatch")
	}
	cluster, desired, created, err := executor.ensureCluster(ctx, state, manifest)
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
	claims, err := executor.buildClaims(state, manifest, cluster, timeline)
	if err != nil {
		return err
	}
	return executor.publishEvidence(ctx, state, claims)
}

// ensureCluster builds deterministic intent, GETs the exact Cluster and POSTs only after 404; create success or conflict returns submitted=true for a later read. Existing objects require identity, modeled spec digest and exact desired annotations; no Cluster update or ambiguous blind re-create occurs here.
// ensureCluster строит детерминированный intent, читает точный Cluster GET и отправляет POST лишь после 404; успех create либо conflict возвращает submitted=true для позднего чтения. Existing objects требуют identity, digest описанного spec и точных desired annotations; обновления Cluster и слепого повтора неопределённого create здесь нет.
func (executor *Executor) ensureCluster(
	ctx context.Context,
	state model.RestoreState,
	manifest BackupManifest,
) (clusterEnvelope, desiredCluster, bool, error) {
	desired, err := executor.desiredCluster(state, manifest)
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
	if err != nil || servedSpecDigest != desiredSpecDigest {
		return clusterEnvelope{}, desired, false, errors.New(
			"CloudNativePG PITR cluster immutable intent mismatch",
		)
	}
	for name, value := range desired.Metadata.Annotations {
		if cluster.Metadata.Annotations[name] != value {
			return clusterEnvelope{}, desired, false, errors.New(
				"CloudNativePG PITR cluster immutable annotation mismatch",
			)
		}
	}
	return cluster, desired, false, nil
}

// desiredCluster validates restore ID and exact manifest digest/target, derives the name from ID prefix and epoch, hashes immutable intent and builds recovery spec from configured instance/image/storage values. It only returns a value and does not create a Cluster or authenticate the supplied state.
// desiredCluster проверяет restore ID и точные manifest digest и target, выводит имя из префикса ID и epoch, хеширует immutable intent и строит recovery spec из настроенных instance, image и storage values. Возвращается лишь значение без создания Cluster и аутентификации переданного state.
func (executor *Executor) desiredCluster(
	state model.RestoreState,
	manifest BackupManifest,
) (desiredCluster, error) {
	if !validRestoreID(state.RestoreID) ||
		manifest.DigestSHA256 != state.BackupManifestDigest ||
		manifest.RecoveryTarget != state.RecoveryTargetUnix {
		return desiredCluster{}, errors.New("PITR restore ID is invalid")
	}
	restorePrefix := strings.ToLower(state.RestoreID[:8])
	name := fmt.Sprintf(
		"internal-rpc-authority-restore-%s-e%d",
		restorePrefix,
		state.RestoreEpoch,
	)
	intent := struct {
		RestoreID             string `json:"restore_id"`
		RestoreEpoch          uint64 `json:"restore_epoch"`
		BackupManifestDigest  string `json:"backup_manifest_digest_sha256"`
		RecoveryTarget        int64  `json:"recovery_target_time"`
		BackupUID             string `json:"backup_uid"`
		BackupResourceVersion string `json:"backup_resource_version"`
		ProviderBackupID      string `json:"provider_backup_id"`
		SourceClusterUID      string `json:"source_cluster_uid"`
		SourceClusterVersion  string `json:"source_cluster_resource_version"`
	}{
		RestoreID: state.RestoreID, RestoreEpoch: state.RestoreEpoch,
		BackupManifestDigest:  state.BackupManifestDigest,
		RecoveryTarget:        state.RecoveryTargetUnix,
		BackupUID:             manifest.BackupUID,
		BackupResourceVersion: manifest.BackupResourceVersion,
		ProviderBackupID:      manifest.ProviderBackupID,
		SourceClusterUID:      manifest.SourceClusterUID,
		SourceClusterVersion:  manifest.SourceClusterResourceVersion,
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
				"kodex.dev/restore-id":                           state.RestoreID,
				"kodex.dev/restore-epoch":                        strconv.FormatUint(state.RestoreEpoch, 10),
				"kodex.dev/restore-intent-digest-sha256":         intentDigest,
				"kodex.dev/backup-manifest-digest-sha256":        manifest.DigestSHA256,
				"kodex.dev/cnpg-backup-uid":                      manifest.BackupUID,
				"kodex.dev/cnpg-backup-resource-version":         manifest.BackupResourceVersion,
				"kodex.dev/cnpg-backup-id":                       manifest.ProviderBackupID,
				"kodex.dev/cnpg-source-cluster-uid":              manifest.SourceClusterUID,
				"kodex.dev/cnpg-source-cluster-resource-version": manifest.SourceClusterResourceVersion,
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
					TargetTLI: strconv.FormatUint(manifest.SourceTimeline, 10),
					BackupID:  manifest.ProviderBackupID,
				},
			}},
			ExternalClusters: []externalCluster{{
				Name: "origin",
				Plugin: externalPlugin{
					Name: barmanPluginName,
					Parameters: map[string]string{
						"barmanObjectName": manifest.BarmanObjectName,
						"serverName":       manifest.BarmanServerName,
					},
				},
			}},
		},
	}, nil
}

// validateHealthyCluster requires matching observed/current generation, exact healthy phase and instance count, the expected primary prefix, Ready condition and a primary-reported timeline matching aggregate status. It returns that timeline without probing PostgreSQL or comparing it with the source timeline.
// validateHealthyCluster требует совпадения observed и current generation, точных healthy phase и числа экземпляров, ожидаемого префикса primary, Ready condition и совпадения primary-reported timeline с общим status. Timeline возвращается без проверки PostgreSQL и сравнения с source timeline.
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

// buildClaims canonically hashes the expected target and accepted ACK sets, copies restore/manifest/provider pins and records an exact next anchor with predecessor at the current second. It constructs claims only; it does not validate every source field, sign or persist them.
// buildClaims канонически хеширует expected target и accepted ACK sets, копирует restore, manifest и provider pins и записывает точный следующий anchor с predecessor и текущей секундой. Строятся только claims без проверки каждого исходного поля, подписи и сохранения.
func (executor *Executor) buildClaims(
	state model.RestoreState,
	manifest BackupManifest,
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
		BackupResourceName:                    manifest.BackupName,
		BackupResourceUID:                     manifest.BackupUID,
		BackupResourceVersion:                 manifest.BackupResourceVersion,
		BackupResourceGeneration:              manifest.BackupGeneration,
		ProviderBackupID:                      manifest.ProviderBackupID,
		ProviderBackupName:                    manifest.ProviderBackupName,
		SourceClusterUID:                      manifest.SourceClusterUID,
		SourceClusterResourceVersion:          manifest.SourceClusterResourceVersion,
		SourceClusterGeneration:               manifest.SourceClusterGeneration,
		SourceClusterSpecSHA256:               manifest.SourceClusterSpecSHA256,
		BarmanObjectName:                      manifest.BarmanObjectName,
		BarmanServerName:                      manifest.BarmanServerName,
		SourceTimelineID:                      manifest.SourceTimeline,
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

// publishEvidence reads the pre-existing evidence Secret, accepts an already matching signed intent or requires predecessor annotations, signs and version-CAS-PUTs one evidence payload, then GETs and checks served evidence. Later failure can follow a persisted PUT; retries must read current state rather than assume rollback.
// publishEvidence читает существующий evidence Secret, принимает уже совпадающий signed intent либо требует predecessor annotations, подписывает и version-CAS-PUT-ит один evidence payload, затем читает GET и проверяет served evidence. Поздний отказ возможен после сохранённого PUT; повтор должен читать текущее состояние, а не предполагать rollback.
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
	if current.Metadata.Annotations["kodex.dev/restore-anchor-revision"] !=
		strconv.FormatUint(state.AnchorRevision, 10) ||
		current.Metadata.Annotations["kodex.dev/restore-evidence-digest-sha256"] !=
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
	current.Metadata.Annotations = evidenceAnnotations(claims, digestHex)
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

// existingEvidenceMatches strictly decodes stored Base64, verifies canonical JWS with this executor's public key, compares normalized intent and checks served annotations against actual signed claims. It does not independently enforce the verifier's complete identity/time/state policy.
// existingEvidenceMatches строго декодирует сохранённый Base64, проверяет canonical JWS публичным ключом этого executor, сравнивает нормализованный intent и served annotations с фактическими signed claims. Полная identity, time и state policy verifier независимо не выполняется.
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

// sameEvidenceIntent zeroes issue/completion times and restored resource version in value copies, then compares canonical bytes of all remaining fields. It intentionally allows those three observations to differ and does not verify either signature.
// sameEvidenceIntent обнуляет issue и completion times и restored resource version в копиях значений, затем сравнивает canonical bytes остальных полей. Этим трём наблюдениям намеренно разрешено различаться; подписи не проверяются.
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

// do reloads the projected token for one caller-selected executor-owned endpoint, sends the context-bound request and reads a bounded response before closing it. It returns HTTP status to the caller and does not retry transport errors or interpret JSON.
// do перечитывает projected token для одного переданного принадлежащего executor endpoint, отправляет запрос с контекстом и читает ограниченный ответ до закрытия. HTTP status возвращается caller без повтора transport errors и интерпретации JSON.
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
		return nil, nil, errors.New("kubernetes PITR response rejected")
	}
	return response, raw, nil
}

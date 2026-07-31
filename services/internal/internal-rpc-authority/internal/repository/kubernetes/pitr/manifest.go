package pitr

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

// ManifestResolverConfig закрепляет exact CNPG Backup и его source Cluster.
type ManifestResolverConfig struct {
	Address          string
	TLSServerName    string
	CAFile           string
	TokenFile        string
	Namespace        string
	BackupName       string
	SourceCluster    string
	BarmanObjectName string
	BarmanServerName string
	Timeout          time.Duration
}

// BackupManifest — канонический readback фактически выбранного CNPG backup.
type BackupManifest struct {
	Version                      int    `json:"v"`
	BackupName                   string `json:"backup_name"`
	BackupUID                    string `json:"backup_uid"`
	BackupResourceVersion        string `json:"backup_resource_version"`
	BackupGeneration             uint64 `json:"backup_generation"`
	SourceClusterName            string `json:"source_cluster_name"`
	SourceClusterUID             string `json:"source_cluster_uid"`
	SourceClusterResourceVersion string `json:"source_cluster_resource_version"`
	SourceClusterGeneration      uint64 `json:"source_cluster_generation"`
	SourceClusterSpecSHA256      string `json:"source_cluster_spec_sha256"`
	BarmanObjectName             string `json:"barman_object_name"`
	BarmanServerName             string `json:"barman_server_name"`
	ProviderBackupID             string `json:"provider_backup_id"`
	ProviderBackupName           string `json:"provider_backup_name"`
	ProviderMetadataSHA256       string `json:"provider_metadata_sha256"`
	PostgresMajorVersion         int32  `json:"postgres_major_version"`
	BeginWAL                     string `json:"begin_wal"`
	EndWAL                       string `json:"end_wal"`
	BeginLSN                     string `json:"begin_lsn"`
	EndLSN                       string `json:"end_lsn"`
	StoppedAt                    int64  `json:"stopped_at"`
	RecoveryTarget               int64  `json:"recovery_target"`
	SourceTimeline               uint64 `json:"source_timeline"`
	DigestSHA256                 string `json:"-"`
}

// ManifestResolver читает authoritative Backup/Cluster из Kubernetes API.
type ManifestResolver struct {
	config      ManifestResolverConfig
	client      *http.Client
	backupsURL  string
	clustersURL string
	now         func() time.Time
}

type backupEnvelope struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   objectMetadata `json:"metadata"`
	Spec       struct {
		Cluster struct {
			Name string `json:"name"`
		} `json:"cluster"`
		Method              string `json:"method"`
		PluginConfiguration struct {
			Name string `json:"name"`
		} `json:"pluginConfiguration"`
	} `json:"spec"`
	Status struct {
		Phase          string          `json:"phase"`
		Method         string          `json:"method"`
		BackupID       string          `json:"backupId"`
		BackupName     string          `json:"backupName"`
		ServerName     string          `json:"serverName"`
		MajorVersion   int32           `json:"majorVersion"`
		BeginWAL       string          `json:"beginWal"`
		EndWAL         string          `json:"endWal"`
		BeginLSN       string          `json:"beginLSN"`
		EndLSN         string          `json:"endLSN"`
		StoppedAt      time.Time       `json:"stoppedAt"`
		PluginMetadata json.RawMessage `json:"pluginMetadata"`
	} `json:"status"`
}

type sourceClusterEnvelope struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   objectMetadata  `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type sourceClusterSpec struct {
	Plugins []struct {
		Name          string            `json:"name"`
		IsWALArchiver bool              `json:"isWALArchiver"`
		Parameters    map[string]string `json:"parameters"`
	} `json:"plugins"`
}

// NewManifestResolver создаёт resolver с exact TLS и projected token.
func NewManifestResolver(config ManifestResolverConfig) (*ManifestResolver, error) {
	if config.Address != "https://kubernetes.default.svc:443" ||
		config.TLSServerName != "kubernetes.default.svc" ||
		config.Namespace != "mattercodex-system" ||
		config.BackupName == "" ||
		config.SourceCluster != "internal-rpc-authority-primary" ||
		config.BarmanObjectName == "" ||
		config.BarmanServerName != config.SourceCluster ||
		config.Timeout <= 0 ||
		config.Timeout > 10*time.Second {
		return nil, errors.New("CNPG backup manifest resolver configuration is invalid")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, errors.New("read Kubernetes API CA")
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("Kubernetes API CA is invalid")
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
		url.PathEscape(config.Namespace)
	return &ManifestResolver{
		config: config,
		client: &http.Client{
			Transport: transport,
			Timeout:   config.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("Kubernetes API redirect is forbidden")
			},
		},
		backupsURL:  base + "/backups/" + url.PathEscape(config.BackupName),
		clustersURL: base + "/clusters/" + url.PathEscape(config.SourceCluster),
		now:         time.Now,
	}, nil
}

// Close закрывает простаивающие Kubernetes API connections.
func (resolver *ManifestResolver) Close() {
	resolver.client.CloseIdleConnections()
}

// Resolve связывает immutable Backup, source Cluster и recovery target.
func (resolver *ManifestResolver) Resolve(
	ctx context.Context,
	recoveryTarget time.Time,
) (BackupManifest, error) {
	backupRaw, err := resolver.read(ctx, resolver.backupsURL)
	if err != nil {
		return BackupManifest{}, err
	}
	clusterRaw, err := resolver.read(ctx, resolver.clustersURL)
	if err != nil {
		return BackupManifest{}, err
	}
	var backup backupEnvelope
	var cluster sourceClusterEnvelope
	if json.Unmarshal(backupRaw, &backup) != nil ||
		json.Unmarshal(clusterRaw, &cluster) != nil ||
		backup.APIVersion != cnpgAPIVersion ||
		backup.Kind != "Backup" ||
		cluster.APIVersion != cnpgAPIVersion ||
		cluster.Kind != "Cluster" ||
		!validObjectMetadata(backup.Metadata, resolver.config.BackupName, resolver.config.Namespace) ||
		!validObjectMetadata(cluster.Metadata, resolver.config.SourceCluster, resolver.config.Namespace) {
		return BackupManifest{}, errors.New("CNPG backup source identity rejected")
	}
	if backup.Spec.Cluster.Name != resolver.config.SourceCluster ||
		backup.Spec.Method != "plugin" ||
		backup.Spec.PluginConfiguration.Name != barmanPluginName ||
		backup.Status.Phase != "completed" ||
		backup.Status.Method != "plugin" ||
		backup.Status.BackupID == "" ||
		backup.Status.BackupName == "" ||
		backup.Status.ServerName != resolver.config.BarmanServerName ||
		backup.Status.MajorVersion <= 0 ||
		backup.Status.BeginLSN == "" ||
		backup.Status.EndLSN == "" ||
		backup.Status.StoppedAt.IsZero() {
		return BackupManifest{}, errors.New("CNPG Backup is incomplete or not immutable plugin evidence")
	}
	var clusterSpec sourceClusterSpec
	if json.Unmarshal(cluster.Spec, &clusterSpec) != nil ||
		!hasExactBarmanPlugin(clusterSpec, resolver.config.BarmanObjectName) {
		return BackupManifest{}, errors.New("CNPG source Cluster plugin binding rejected")
	}
	timeline, err := exactWALTimeline(backup.Status.BeginWAL, backup.Status.EndWAL)
	if err != nil {
		return BackupManifest{}, err
	}
	recoveryTarget = recoveryTarget.UTC().Truncate(time.Second)
	stoppedAt := backup.Status.StoppedAt.UTC().Truncate(time.Second)
	if recoveryTarget.Before(stoppedAt) ||
		recoveryTarget.After(resolver.now().UTC().Add(30*time.Second)) {
		return BackupManifest{}, errors.New("CNPG recovery target is outside the authoritative backup window")
	}
	sourceSpecDigest, err := internalrpcauth.CanonicalJSONSHA256(cluster.Spec)
	if err != nil {
		return BackupManifest{}, errors.New("digest CNPG source Cluster spec")
	}
	metadata := backup.Status.PluginMetadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	metadataDigest, err := internalrpcauth.CanonicalJSONSHA256(metadata)
	if err != nil {
		return BackupManifest{}, errors.New("digest CNPG provider metadata")
	}
	manifest := BackupManifest{
		Version:    1,
		BackupName: backup.Metadata.Name, BackupUID: backup.Metadata.UID,
		BackupResourceVersion: backup.Metadata.ResourceVersion,
		BackupGeneration:      backup.Metadata.Generation,
		SourceClusterName:     cluster.Metadata.Name, SourceClusterUID: cluster.Metadata.UID,
		SourceClusterResourceVersion: cluster.Metadata.ResourceVersion,
		SourceClusterGeneration:      cluster.Metadata.Generation,
		SourceClusterSpecSHA256:      sourceSpecDigest,
		BarmanObjectName:             resolver.config.BarmanObjectName,
		BarmanServerName:             backup.Status.ServerName,
		ProviderBackupID:             backup.Status.BackupID,
		ProviderBackupName:           backup.Status.BackupName,
		ProviderMetadataSHA256:       metadataDigest,
		PostgresMajorVersion:         backup.Status.MajorVersion,
		BeginWAL:                     backup.Status.BeginWAL, EndWAL: backup.Status.EndWAL,
		BeginLSN: backup.Status.BeginLSN, EndLSN: backup.Status.EndLSN,
		StoppedAt: stoppedAt.Unix(), RecoveryTarget: recoveryTarget.Unix(),
		SourceTimeline: timeline,
	}
	manifest.DigestSHA256, err = internalrpcauth.CanonicalJSONSHA256(manifest)
	if err != nil {
		return BackupManifest{}, errors.New("digest authoritative CNPG backup manifest")
	}
	return manifest, nil
}

func (resolver *ManifestResolver) read(ctx context.Context, endpoint string) ([]byte, error) {
	token, err := readToken(resolver.config.TokenFile)
	if err != nil {
		return nil, errors.New("read Kubernetes API workload token")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("construct CNPG manifest request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := resolver.client.Do(request)
	if err != nil {
		return nil, errors.New("read authoritative CNPG manifest")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || response.StatusCode != http.StatusOK ||
		len(raw) == 0 || len(raw) > maxResponseBytes {
		return nil, errors.New("authoritative CNPG manifest response rejected")
	}
	return raw, nil
}

func validObjectMetadata(metadata objectMetadata, name string, namespace string) bool {
	return metadata.Name == name &&
		metadata.Namespace == namespace &&
		len(metadata.UID) >= 8 &&
		len(metadata.UID) <= 128 &&
		validResourceVersion(metadata.ResourceVersion) &&
		metadata.Generation > 0
}

func hasExactBarmanPlugin(spec sourceClusterSpec, objectName string) bool {
	matches := 0
	for _, plugin := range spec.Plugins {
		if plugin.Name != barmanPluginName {
			continue
		}
		if !plugin.IsWALArchiver ||
			plugin.Parameters["barmanObjectName"] != objectName {
			return false
		}
		matches++
	}
	return matches == 1
}

func exactWALTimeline(beginWAL string, endWAL string) (uint64, error) {
	if len(beginWAL) != 24 || len(endWAL) != 24 {
		return 0, errors.New("CNPG WAL identity is invalid")
	}
	beginRaw, beginErr := hex.DecodeString(beginWAL)
	endRaw, endErr := hex.DecodeString(endWAL)
	if beginErr != nil || endErr != nil ||
		len(beginRaw) != 12 || len(endRaw) != 12 ||
		!strings.EqualFold(beginWAL[:8], endWAL[:8]) {
		return 0, errors.New("CNPG WAL timeline binding rejected")
	}
	timeline, err := strconv.ParseUint(beginWAL[:8], 16, 32)
	if err != nil || timeline == 0 {
		return 0, errors.New("CNPG WAL timeline is invalid")
	}
	return timeline, nil
}

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

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// ManifestResolverConfig pins the in-cluster API/TLS boundary, selected Backup, fixed source Cluster, Barman object/server and bounded request timeout. File paths identify mounted credentials rather than embedded secret values.
// ManifestResolverConfig закрепляет внутрикластерные API и TLS boundary, выбранный Backup, фиксированный source Cluster, объект и сервер Barman и ограниченный timeout. Пути обозначают смонтированные credentials, а не содержат секретные значения.
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

// BackupManifest records the selected Backup and source Cluster identities, versions, provider/WAL metadata and recovery target for canonical hashing. DigestSHA256 is excluded from its own encoding; the manifest is an observed snapshot, not a signature or atomic multi-resource read.
// BackupManifest фиксирует идентичности и версии выбранных Backup и source Cluster, provider и WAL metadata и recovery target для canonical hashing. DigestSHA256 исключён из собственного кодирования; манифест является наблюдённым снимком, а не подписью и атомарным чтением нескольких ресурсов.
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

// ManifestResolver owns an HTTP client and fixed Backup/Cluster URLs, plus a clock for recovery-window validation. It reads API metadata but does not retrieve backup objects or execute recovery.
// ManifestResolver владеет HTTP-клиентом и фиксированными URL Backup и Cluster и часами проверки recovery window. Читаются метаданные API, но объекты резервной копии не загружаются и восстановление не выполняется.
type ManifestResolver struct {
	config      ManifestResolverConfig
	client      *http.Client
	backupsURL  string
	clustersURL string
	now         func() time.Time
}

// backupEnvelope models the Backup identity, plugin binding and completion/provider metadata consumed by Resolve. Ordinary JSON decoding permits unmodeled fields; explicit resolver checks establish required values.
// backupEnvelope описывает идентичность Backup, plugin binding и используемые Resolve completion и provider metadata. Обычный JSON-декодер допускает неописанные поля; необходимые значения устанавливают явные проверки resolver.
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

// sourceClusterEnvelope keeps source Cluster identity separate from raw spec bytes so Resolve can validate the plugin and canonically hash the full observed spec.
// sourceClusterEnvelope отделяет идентичность source Cluster от исходных spec-байтов, позволяя Resolve проверить plugin и канонически хешировать весь наблюдённый spec.
type sourceClusterEnvelope struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   objectMetadata  `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

// sourceClusterSpec exposes plugin names, WAL-archiver flags and parameter maps used to require one correctly bound Barman plugin while allowing other plugin entries.
// sourceClusterSpec раскрывает имена plugins, признаки WAL archiver и карты параметров для требования одного правильно привязанного Barman plugin при допустимости других plugins.
type sourceClusterSpec struct {
	Plugins []struct {
		Name          string            `json:"name"`
		IsWALArchiver bool              `json:"isWALArchiver"`
		Parameters    map[string]string `json:"parameters"`
	} `json:"plugins"`
}

// NewManifestResolver validates the fixed API/TLS/namespace/source/server and nonempty Backup/object names, loads the CA and configures TLS 1.3, timeout and redirect rejection. It builds exact escaped URLs without reading a token or making an API call.
// NewManifestResolver проверяет фиксированные API, TLS, namespace, source и server и непустые имена Backup и object, загружает CA и задаёт TLS 1.3, timeout и запрет redirect. Точные URL с экранированием строятся без чтения токена и вызова API.
func NewManifestResolver(config ManifestResolverConfig) (*ManifestResolver, error) {
	if config.Address != "https://kubernetes.default.svc:443" ||
		config.TLSServerName != "kubernetes.default.svc" ||
		config.Namespace != "kodex-system" ||
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

// Close closes idle resolver HTTP connections without cancelling active caller contexts, deleting a Backup or changing source Cluster state.
// Close закрывает простаивающие HTTP-соединения resolver без отмены активных контекстов вызывающих, удаления Backup и изменения состояния source Cluster.
func (resolver *ManifestResolver) Close() {
	resolver.client.CloseIdleConnections()
}

// Resolve sequentially reads the selected Backup and source Cluster, validates their identities, completed plugin metadata and one shared nonzero WAL timeline, bounds the second-truncated recovery target, and canonically hashes the resulting manifest. It does not lock both resources, read WAL bytes or cryptographically sign the result.
// Resolve последовательно читает выбранные Backup и source Cluster, проверяет идентичности, completed plugin metadata и общий ненулевой WAL timeline, ограничивает recovery target с точностью секунды и канонически хеширует манифест. Оба ресурса не блокируются, байты WAL не читаются и результат криптографически не подписывается.
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

// read loads the current safe projected token, performs one authenticated GET to a resolver-owned URL and requires HTTP 200 with a nonempty bounded body. It closes the body and rejects transport/read/size errors without retrying or following redirects.
// read загружает текущий безопасный projected token, выполняет один аутентифицированный GET принадлежащего resolver URL и требует HTTP 200 с непустым ограниченным телом. Тело закрывается; transport, read и size errors отклоняются без повтора и перехода по redirect.
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

// validObjectMetadata requires the configured name/namespace, an eight-to-128-byte UID, positive decimal resource version and nonzero generation; it does not authenticate an arbitrary metadata object.
// validObjectMetadata требует настроенные имя и namespace, UID от восьми до 128 байт, положительную десятичную resource version и ненулевую generation; произвольный metadata object не аутентифицируется.
func validObjectMetadata(metadata objectMetadata, name string, namespace string) bool {
	return metadata.Name == name &&
		metadata.Namespace == namespace &&
		len(metadata.UID) >= 8 &&
		len(metadata.UID) <= 128 &&
		validResourceVersion(metadata.ResourceVersion) &&
		metadata.Generation > 0
}

// hasExactBarmanPlugin requires exactly one Barman-named plugin with WAL archiving enabled and the exact object parameter. Other plugin names are ignored, but a malformed or duplicate Barman entry rejects the spec.
// hasExactBarmanPlugin требует ровно один plugin с именем Barman, включённым WAL archiving и точным object parameter. Другие имена plugins игнорируются, но неверная либо повторная Barman entry отклоняет spec.
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

// exactWALTimeline requires two 24-character hexadecimal WAL names sharing the same nonzero first-eight-digit timeline, case-insensitively. It does not compare segment order, validate LSN values or prove WAL archive availability.
// exactWALTimeline требует два 24-символьных hex-имени WAL с одинаковым ненулевым timeline первых восьми цифр без учёта регистра. Порядок сегментов, значения LSN и доступность архива WAL не проверяются.
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

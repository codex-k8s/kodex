// Package configspec parses bounded, protected backup and restore specifications before use.
// RU: Package configspec разбирает ограниченные защищённые спецификации резервирования и восстановления до применения.
package configspec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/securefile"
)

// maximumSpecBytes bounds each protected JSON specification before decoding.
// RU: maximumSpecBytes ограничивает размер каждой защищённой JSON-спецификации до декодирования.
const maximumSpecBytes = 256 << 10

// namePattern permits short DNS-style logical names in backup and restore specifications.
// RU: namePattern допускает короткие логические имена в стиле DNS для спецификаций резервирования и восстановления.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// S3 describes one destination or source bucket and its transport credentials.
// RU: S3 описывает конечный или исходный bucket вместе с реквизитами транспорта.
type S3 struct {
	Name               string `json:"name"`
	Endpoint           string `json:"endpoint"`
	Region             string `json:"region"`
	Bucket             string `json:"bucket"`
	Prefix             string `json:"prefix,omitempty"`
	AccessKeyID        string `json:"accessKeyId"`
	SecretAccessKey    string `json:"secretAccessKey"`
	SessionToken       string `json:"sessionToken,omitempty"`
	UsePathStyle       bool   `json:"usePathStyle"`
	AllowInsecureLocal bool   `json:"allowInsecureLocal,omitempty"`
}

// Database describes a PostgreSQL source, including its schema-version strategy and TLS material.
// RU: Database описывает исходную PostgreSQL-базу, способ определения версии схемы и материалы TLS.
type Database struct {
	Name                  string `json:"name"`
	Host                  string `json:"host"`
	Port                  uint16 `json:"port"`
	Database              string `json:"database"`
	User                  string `json:"user"`
	Password              string `json:"password"`
	Role                  string `json:"role,omitempty"`
	TLSMode               string `json:"tlsMode"`
	TLSServerName         string `json:"tlsServerName,omitempty"`
	CAFile                string `json:"caFile,omitempty"`
	ClientCertificateFile string `json:"clientCertificateFile,omitempty"`
	ClientPrivateKeyFile  string `json:"clientPrivateKeyFile,omitempty"`
	SchemaKind            string `json:"schemaKind"`
	DeclaredSchemaVersion string `json:"declaredSchemaVersion,omitempty"`
}

// Credentials groups a backup destination with bounded database and object-store sources.
// RU: Credentials объединяет место сохранения с ограниченными наборами исходных БД и объектных хранилищ.
type Credentials struct {
	SchemaVersion int        `json:"schemaVersion"`
	Destination   S3         `json:"destination"`
	Databases     []Database `json:"databases"`
	ObjectStores  []S3       `json:"objectStores"`
}

// RepositoryCredentials contains the separately protected destination used for backup readback.
// RU: RepositoryCredentials содержит отдельно защищённое место хранения для чтения резервной копии.
type RepositoryCredentials struct {
	SchemaVersion int `json:"schemaVersion"`
	Destination   S3  `json:"destination"`
}

// RestoreDatabase describes a restore target with a separate administrative database and TLS settings.
// RU: RestoreDatabase описывает целевую БД с отдельной административной БД и настройками TLS.
type RestoreDatabase struct {
	Name                  string `json:"name"`
	Host                  string `json:"host"`
	Port                  uint16 `json:"port"`
	AdminDatabase         string `json:"adminDatabase"`
	Database              string `json:"database"`
	User                  string `json:"user"`
	Password              string `json:"password"`
	TLSMode               string `json:"tlsMode"`
	TLSServerName         string `json:"tlsServerName,omitempty"`
	CAFile                string `json:"caFile,omitempty"`
	ClientCertificateFile string `json:"clientCertificateFile,omitempty"`
	ClientPrivateKeyFile  string `json:"clientPrivateKeyFile,omitempty"`
}

// RestoreTargets binds the declared database and object-store targets of one restore operation.
// RU: RestoreTargets связывает объявленные цели БД и объектного хранилища одной операции восстановления.
type RestoreTargets struct {
	SchemaVersion int               `json:"schemaVersion"`
	Databases     []RestoreDatabase `json:"databases"`
	ObjectStore   S3                `json:"objectStore"`
}

// RestoreApproval binds an expiring approval to one backup, restore and exact target-set digest.
// RU: RestoreApproval связывает ограниченное сроком подтверждение с копией, восстановлением и точным дайджестом набора целей.
type RestoreApproval struct {
	SchemaVersion   int       `json:"schemaVersion"`
	ApprovalID      string    `json:"approvalId"`
	RestoreID       string    `json:"restoreId"`
	BackupID        string    `json:"backupId"`
	TargetSetSHA256 string    `json:"targetSetSha256"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

// LoadCredentials reads one protected backup specification, validates each source and rejects duplicate logical names.
// RU: LoadCredentials читает защищённую спецификацию копирования, проверяет каждый источник и отклоняет повторные логические имена.
func LoadCredentials(path, environment string) (Credentials, error) {
	var value Credentials
	if err := read(path, &value); err != nil {
		return Credentials{}, err
	}
	if value.SchemaVersion != 1 || len(value.Databases) == 0 || len(value.Databases) > 16 ||
		len(value.ObjectStores) == 0 || len(value.ObjectStores) > 16 {
		return Credentials{}, errors.New("backup credentials schema is invalid")
	}
	if err := value.Destination.Validate(environment); err != nil {
		return Credentials{}, err
	}
	seen := map[string]struct{}{}
	for _, database := range value.Databases {
		if err := database.Validate(environment); err != nil {
			return Credentials{}, err
		}
		if _, exists := seen["database:"+database.Name]; exists {
			return Credentials{}, errors.New("backup database name is duplicated")
		}
		seen["database:"+database.Name] = struct{}{}
	}
	for _, store := range value.ObjectStores {
		if err := store.Validate(environment); err != nil {
			return Credentials{}, err
		}
		if _, exists := seen["store:"+store.Name]; exists {
			return Credentials{}, errors.New("backup object store name is duplicated")
		}
		seen["store:"+store.Name] = struct{}{}
	}
	return value, nil
}

// LoadRestoreTargets validates bounded targets and rejects duplicate database names before restore use.
// RU: LoadRestoreTargets проверяет ограниченный набор целей и повторные имена БД до восстановления.
func LoadRestoreTargets(path, environment string) (RestoreTargets, error) {
	var value RestoreTargets
	if err := read(path, &value); err != nil {
		return RestoreTargets{}, err
	}
	if value.SchemaVersion != 1 || len(value.Databases) == 0 || len(value.Databases) > 16 {
		return RestoreTargets{}, errors.New("restore target schema is invalid")
	}
	if err := value.ObjectStore.Validate(environment); err != nil {
		return RestoreTargets{}, err
	}
	seen := map[string]struct{}{}
	for _, database := range value.Databases {
		if err := database.Validate(environment); err != nil {
			return RestoreTargets{}, err
		}
		if _, exists := seen[database.Name]; exists {
			return RestoreTargets{}, errors.New("restore database name is duplicated")
		}
		seen[database.Name] = struct{}{}
	}
	return value, nil
}

// LoadRepositoryCredentials reads and validates the independently protected backup repository destination.
// RU: LoadRepositoryCredentials читает и проверяет отдельно защищённое место хранения резервных копий.
func LoadRepositoryCredentials(path, environment string) (RepositoryCredentials, error) {
	var value RepositoryCredentials
	if err := read(path, &value); err != nil {
		return RepositoryCredentials{}, err
	}
	if value.SchemaVersion != 1 {
		return RepositoryCredentials{}, errors.New("backup repository credential schema is invalid")
	}
	if err := value.Destination.Validate(environment); err != nil {
		return RepositoryCredentials{}, err
	}
	return value, nil
}

// LoadRestoreApproval accepts only a current, well-formed approval expiring within 24 hours of now.
// RU: LoadRestoreApproval принимает лишь корректное действующее подтверждение со сроком не дальше 24 часов от now.
func LoadRestoreApproval(path string, now time.Time) (RestoreApproval, error) {
	var value RestoreApproval
	if err := read(path, &value); err != nil {
		return RestoreApproval{}, err
	}
	if value.SchemaVersion != 1 || !namePattern.MatchString(value.ApprovalID) ||
		!namePattern.MatchString(value.RestoreID) || !validBackupID(value.BackupID) ||
		!validDigest(value.TargetSetSHA256) || value.ExpiresAt.IsZero() || !now.Before(value.ExpiresAt) ||
		value.ExpiresAt.Sub(now) > 24*time.Hour {
		return RestoreApproval{}, errors.New("restore approval is invalid or expired")
	}
	return value, nil
}

// FingerprintTargets hashes canonical target identities and transport choices, excluding credentials and database order.
// RU: FingerprintTargets хеширует канонические идентичности целей и транспортные настройки без реквизитов и зависимости от порядка БД.
func FingerprintTargets(targets RestoreTargets) (string, error) {
	// databaseFingerprint keeps only target identity and TLS choices, never passwords.
	// RU: databaseFingerprint хранит лишь идентичность цели и выбор TLS, но не пароли.
	type databaseFingerprint struct {
		Name, Host, AdminDatabase, Database, User, TLSMode, TLSServerName, CAFile string
		Port                                                                      uint16
	}
	// storeFingerprint binds the object-store target without access or secret keys.
	// RU: storeFingerprint связывает цель объектного хранилища без открытого и секретного ключей.
	type storeFingerprint struct {
		Name, Endpoint, Region, Bucket, Prefix string
		UsePathStyle                           bool
	}
	databases := make([]databaseFingerprint, 0, len(targets.Databases))
	for _, database := range targets.Databases {
		databases = append(databases, databaseFingerprint{
			Name: database.Name, Host: database.Host, Port: database.Port,
			AdminDatabase: database.AdminDatabase, Database: database.Database, User: database.User,
			TLSMode: database.TLSMode, TLSServerName: database.TLSServerName, CAFile: database.CAFile,
		})
	}
	sort.Slice(databases, func(i, j int) bool { return databases[i].Name < databases[j].Name })
	payload, err := json.Marshal(struct {
		SchemaVersion int                   `json:"schemaVersion"`
		Databases     []databaseFingerprint `json:"databases"`
		ObjectStore   storeFingerprint      `json:"objectStore"`
	}{
		SchemaVersion: targets.SchemaVersion,
		Databases:     databases,
		ObjectStore: storeFingerprint{
			Name: targets.ObjectStore.Name, Endpoint: targets.ObjectStore.Endpoint,
			Region: targets.ObjectStore.Region, Bucket: targets.ObjectStore.Bucket,
			Prefix: targets.ObjectStore.Prefix, UsePathStyle: targets.ObjectStore.UsePathStyle,
		},
	})
	if err != nil {
		return "", errors.New("encode restore target fingerprint")
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// Validate checks S3 identity and permits plaintext only for an explicitly local staging service.
// RU: Validate проверяет идентичность S3 и допускает открытый транспорт лишь для явно локального staging-сервиса.
func (value S3) Validate(environment string) error {
	endpoint, err := url.Parse(value.Endpoint)
	if err != nil || !namePattern.MatchString(value.Name) || value.Region == "" || value.Bucket == "" ||
		value.AccessKeyID == "" || value.SecretAccessKey == "" || endpoint.User != nil || endpoint.Host == "" ||
		(endpoint.Path != "" && endpoint.Path != "/") || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return errors.New("S3 credential configuration is invalid")
	}
	if endpoint.Scheme == "https" {
		return nil
	}
	host := endpoint.Hostname()
	if endpoint.Scheme != "http" || environment != "staging" || !value.AllowInsecureLocal ||
		!strings.HasSuffix(host, ".kodex-system.svc.cluster.local") {
		return errors.New("insecure S3 endpoint is forbidden")
	}
	return nil
}

// Validate checks a backup source's identity, schema strategy and exact PostgreSQL TLS profile.
// RU: Validate проверяет исходную БД, стратегию версии схемы и точный профиль PostgreSQL TLS.
func (value Database) Validate(environment string) error {
	if !namePattern.MatchString(value.Name) || !validHost(value.Host) || value.Port == 0 ||
		!validIdentifier(value.Database) || !validIdentifier(value.User) || value.Password == "" ||
		(value.Role != "" && !validIdentifier(value.Role)) ||
		(value.SchemaKind != "goose" && value.SchemaKind != "declared") ||
		(value.SchemaKind == "declared" && strings.TrimSpace(value.DeclaredSchemaVersion) == "") {
		return errors.New("backup database configuration is invalid")
	}
	return validateTLS(environment, value.TLSMode, value.Host, value.TLSServerName, value.CAFile,
		value.ClientCertificateFile, value.ClientPrivateKeyFile)
}

// Validate checks a restore target and forbids using its administrative database as the target.
// RU: Validate проверяет цель восстановления и запрещает использовать административную БД как целевую.
func (value RestoreDatabase) Validate(environment string) error {
	if !namePattern.MatchString(value.Name) || !validHost(value.Host) || value.Port == 0 ||
		!validIdentifier(value.AdminDatabase) || !validIdentifier(value.Database) ||
		!validIdentifier(value.User) || value.Password == "" || value.Database == value.AdminDatabase {
		return errors.New("restore database configuration is invalid")
	}
	return validateTLS(environment, value.TLSMode, value.Host, value.TLSServerName, value.CAFile,
		value.ClientCertificateFile, value.ClientPrivateKeyFile)
}

// validateTLS permits staging-local plaintext only at the named service and otherwise requires verified TLS paths.
// RU: validateTLS допускает открытый транспорт лишь к указанному staging-сервису, иначе требует проверяемые пути TLS.
func validateTLS(environment, mode, host, serverName, caFile, certificateFile, keyFile string) error {
	if mode == "disable" {
		if environment == "staging" && strings.HasSuffix(host, ".kodex-system.svc.cluster.local") {
			return nil
		}
		return errors.New("plaintext PostgreSQL transport is forbidden")
	}
	if mode != "verify-full" || serverName == "" || caFile == "" || !validHost(serverName) {
		return errors.New("PostgreSQL TLS configuration is invalid")
	}
	paths := []string{caFile}
	if certificateFile != "" || keyFile != "" {
		if certificateFile == "" || keyFile == "" {
			return errors.New("PostgreSQL client TLS configuration is incomplete")
		}
		paths = append(paths, certificateFile, keyFile)
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("PostgreSQL TLS path is invalid")
		}
	}
	return nil
}

// read decodes one bounded protected JSON file with unknown fields and trailing values rejected.
// RU: read декодирует один ограниченный защищённый JSON-файл, отклоняя неизвестные поля и дополнительные значения.
func read(path string, target any) error {
	value, err := securefile.Read(path, maximumSpecBytes)
	if err != nil {
		return errors.New("read protected configuration")
	}
	defer clear(value)
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("decode protected configuration")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("decode protected configuration")
	}
	return nil
}

// validHost accepts an IP literal or restricted DNS-like hostname without URI or wildcard syntax.
// RU: validHost принимает IP-адрес либо ограниченное DNS-имя без синтаксиса URI и wildcard.
func validHost(value string) bool {
	if value == "" || strings.ContainsAny(value, "*/:@ ") {
		return false
	}
	return net.ParseIP(value) != nil || regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?$`).MatchString(value)
}

// validIdentifier checks the bounded SQL-oriented name grammar used by database fields.
// RU: validIdentifier проверяет ограниченную грамматику имён SQL-полей базы данных.
func validIdentifier(value string) bool {
	return regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,62}$`).MatchString(value)
}

// validDigest requires a full hexadecimal SHA-256 value with its algorithm prefix.
// RU: validDigest требует полный шестнадцатеричный SHA-256 с префиксом алгоритма.
func validDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

// validBackupID recognizes the fixed timestamp-and-random-suffix backup identifier.
// RU: validBackupID распознаёт фиксированный идентификатор копии из времени и случайного суффикса.
func validBackupID(value string) bool {
	return regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-f0-9]{16}$`).MatchString(value)
}

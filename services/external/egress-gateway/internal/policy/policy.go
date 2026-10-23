// Package policy загружает immutable machine policy egress gateway.
package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	shared "github.com/codex-k8s/kodex/libs/go/mailpolicy"
)

// Policy constants pin the API shape, file bound, and closed STT profile.
// Константы policy закрепляют формат API, предел файла и закрытый профиль STT.
const (
	APIVersion       = "kodex.io/v1alpha1"
	Kind             = "EgressGatewayPolicy"
	MaximumFileBytes = 64 << 10
	STTProfileName   = "openai-stt"
	STTWorkload      = "stt-tts-service"
	STTOperation     = "openai.transcription"
)

// Document is the versioned machine policy contract loaded from JSON.
// Document — версионированный контракт машинной политики из JSON.
type Document struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
}

// Metadata carries the fixed policy name and immutable revision.
// Metadata задаёт фиксированное имя политики и неизменяемую ревизию.
type Metadata struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

// Spec binds DNS rules, runtime limits, destinations, and optional profiles.
// Spec задаёт DNS, лимиты выполнения, адреса назначения и профили.
type Spec struct {
	DNS          DNSConfig     `json:"dns"`
	Limits       Limits        `json:"limits"`
	Destinations []Destination `json:"destinations"`
	Profiles     []Profile     `json:"profiles,omitempty"`
}

// Profile binds a separate listener to a closed workload and operation.
// Profile связывает отдельный listener с закрытой нагрузкой и операцией.
type Profile struct {
	Name         string        `json:"name"`
	Workload     string        `json:"workload"`
	Operation    string        `json:"operation"`
	Destinations []Destination `json:"destinations"`
}

// DNSConfig reuses the validated resolver and cache bounds.
// DNSConfig использует проверяемые пределы resolver и его кэша.
type DNSConfig = dnsresolver.Config

// Limits bounds CONNECT, ClientHello, dial, tunnel, and shutdown resources.
// Limits ограничивает ресурсы CONNECT, ClientHello, dial, tunnel и остановки.
type Limits struct {
	MaximumHeaderBytes             int `json:"maximumHeaderBytes"`
	MaximumClientHelloBytes        int `json:"maximumClientHelloBytes"`
	MaximumConnections             int `json:"maximumConnections"`
	MaximumConnectionsPerSource    int `json:"maximumConnectionsPerSource"`
	HeaderTimeoutMilliseconds      int `json:"headerTimeoutMilliseconds"`
	ClientHelloTimeoutMilliseconds int `json:"clientHelloTimeoutMilliseconds"`
	DialTimeoutMilliseconds        int `json:"dialTimeoutMilliseconds"`
	IdleTimeoutMilliseconds        int `json:"idleTimeoutMilliseconds"`
	WriteTimeoutMilliseconds       int `json:"writeTimeoutMilliseconds"`
	ShutdownTimeoutMilliseconds    int `json:"shutdownTimeoutMilliseconds"`
}

// Destination is an exact normalized FQDN with its required TCP port.
// Destination — точный нормализованный FQDN и обязательный TCP-порт.
type Destination struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
}

// Active is a validated immutable policy snapshot with an exact allowlist.
// Active — проверенный неизменяемый снимок policy с точным списком разрешений.
type Active struct {
	document Document
	digest   string
	allowed  map[string]struct{}
	profile  *Profile
}

// LoadFile reads a bounded file and validates its expected revision and digest.
// LoadFile ограниченно читает файл и сверяет ожидаемые ревизию и дайджест.
func LoadFile(path, expectedRevision, expectedDigest string) (*Active, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("open policy file")
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, MaximumFileBytes+1))
	if err != nil {
		return nil, errors.New("read policy file")
	}
	if len(value) == 0 || len(value) > MaximumFileBytes {
		return nil, errors.New("policy file size is invalid")
	}
	return Load(value, expectedRevision, expectedDigest)
}

// Load strictly parses policy bytes and builds a validated active snapshot.
// Load строго разбирает байты policy и создаёт проверенный активный снимок.
func Load(value []byte, expectedRevision, expectedDigest string) (*Active, error) {
	document, digest, err := parseAndDigest(value)
	if err != nil {
		return nil, err
	}
	if expectedRevision == "" || document.Metadata.Revision != expectedRevision {
		return nil, errors.New("policy revision does not match expected revision")
	}
	if len(expectedDigest) != sha256.Size*2 || digest != expectedDigest {
		return nil, errors.New("policy digest does not match expected digest")
	}
	allowed := make(map[string]struct{}, len(document.Spec.Destinations))
	for _, destination := range document.Spec.Destinations {
		allowed[net.JoinHostPort(destination.Hostname, strconv.Itoa(destination.Port))] = struct{}{}
	}
	return &Active{document: document, digest: digest, allowed: allowed}, nil
}

// DigestFile computes the canonical digest with the same parser used at runtime.
// DigestFile вычисляет канонический дайджест тем же разборщиком, что и runtime.
func DigestFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("open policy file")
	}
	defer file.Close()
	value, err := io.ReadAll(io.LimitReader(file, MaximumFileBytes+1))
	if err != nil {
		return "", errors.New("read policy file")
	}
	_, digest, err := parseAndDigest(value)
	return digest, err
}

// parseAndDigest rejects ambiguous JSON before validating and hashing canonical fields.
// parseAndDigest отклоняет неоднозначный JSON до проверки и хеширования полей.
func parseAndDigest(value []byte) (Document, string, error) {
	if len(value) == 0 || len(value) > MaximumFileBytes {
		return Document{}, "", errors.New("policy size is invalid")
	}
	if err := rejectDuplicateFields(value); err != nil {
		return Document{}, "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, "", errors.New("policy JSON is invalid")
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Document{}, "", err
	}
	if err := validate(&document); err != nil {
		return Document{}, "", err
	}
	canonical, err := canonicalJSON(document)
	if err != nil {
		return Document{}, "", errors.New("canonicalize policy")
	}
	digestValue := sha256.Sum256(canonical)
	digest := hex.EncodeToString(digestValue[:])
	return document, digest, nil
}

// Revision returns the active immutable policy revision.
// Revision возвращает активную неизменяемую ревизию политики.
func (active *Active) Revision() string { return active.document.Metadata.Revision }

// Digest returns the canonical SHA-256 as unprefixed lowercase hexadecimal.
// Digest возвращает канонический SHA-256 шестнадцатеричной строкой без префикса.
func (active *Active) Digest() string { return active.digest }

// DNS returns the validated DNS resolution limits.
// DNS возвращает проверенные пределы разрешения DNS.
func (active *Active) DNS() DNSConfig { return active.document.Spec.DNS }

// Limits returns the validated runtime resource bounds.
// Limits возвращает проверенные пределы ресурсов выполнения.
func (active *Active) Limits() Limits { return active.document.Spec.Limits }

// Destinations copies the exact allowlist for the active profile or default policy.
// Destinations копирует точный список разрешений профиля либо основной политики.
func (active *Active) Destinations() []Destination {
	destinations := active.document.Spec.Destinations
	if active.profile != nil {
		destinations = active.profile.Destinations
	}
	result := make([]Destination, len(destinations))
	copy(result, destinations)
	return result
}

// ForProfile selects only a registered profile from this policy snapshot.
// ForProfile выбирает только зарегистрированный профиль того же снимка policy.
func (active *Active) ForProfile(name string) (*Active, error) {
	for _, profile := range active.document.Spec.Profiles {
		if profile.Name != name {
			continue
		}
		allowed := make(map[string]struct{}, len(profile.Destinations))
		for _, destination := range profile.Destinations {
			allowed[net.JoinHostPort(destination.Hostname, strconv.Itoa(destination.Port))] = struct{}{}
		}
		return &Active{document: active.document, digest: active.digest, allowed: allowed, profile: &profile}, nil
	}
	return nil, errors.New("egress policy profile is not registered")
}

// ProfileIdentity returns identities from the selected, validated profile only.
// ProfileIdentity возвращает идентификаторы только проверенного выбранного профиля.
func (active *Active) ProfileIdentity() (name, workload, operation string) {
	if active.profile != nil {
		return active.profile.Name, active.profile.Workload, active.profile.Operation
	}
	return "default", "", ""
}

// Allows checks one exact normalized hostname and port against this snapshot.
// Allows сверяет точные нормализованные имя узла и порт с текущим снимком.
func (active *Active) Allows(hostname string, port int) bool {
	_, ok := active.allowed[net.JoinHostPort(hostname, strconv.Itoa(port))]
	return ok
}

// NormalizeHostname accepts only already canonical lowercase ASCII FQDNs.
// NormalizeHostname принимает только канонические FQDN в нижнем регистре ASCII.
func NormalizeHostname(value string) (string, error) {
	return shared.NormalizeHostname(value)
}

// validate closes the policy identity, limits, destinations, and STT profile registry.
// validate ограничивает идентичность, лимиты, адреса и реестр профилей STT.
func validate(document *Document) error {
	if document.APIVersion != APIVersion || document.Kind != Kind || document.Metadata.Name != "egress-gateway" {
		return errors.New("policy identity is invalid")
	}
	if !validRevision(document.Metadata.Revision) {
		return errors.New("policy revision is invalid")
	}
	if document.Spec.DNS.Validate() != nil {
		return errors.New("policy DNS bounds are invalid")
	}
	limits := document.Spec.Limits
	if limits.MaximumHeaderBytes < 1024 || limits.MaximumHeaderBytes > 64<<10 ||
		limits.MaximumClientHelloBytes < 1024 || limits.MaximumClientHelloBytes > 128<<10 ||
		limits.MaximumConnections < 8 || limits.MaximumConnections > 4096 ||
		limits.MaximumConnectionsPerSource < 1 || limits.MaximumConnectionsPerSource > limits.MaximumConnections ||
		!boundedMilliseconds(limits.HeaderTimeoutMilliseconds, 500, 30_000) ||
		!boundedMilliseconds(limits.ClientHelloTimeoutMilliseconds, 500, 30_000) ||
		!boundedMilliseconds(limits.DialTimeoutMilliseconds, 100, 30_000) ||
		!boundedMilliseconds(limits.IdleTimeoutMilliseconds, 1_000, int((30*time.Minute)/time.Millisecond)) ||
		!boundedMilliseconds(limits.WriteTimeoutMilliseconds, 100, 30_000) ||
		!boundedMilliseconds(limits.ShutdownTimeoutMilliseconds, 1_000, 20_000) {
		return errors.New("policy runtime bounds are invalid")
	}
	if len(document.Spec.Destinations) == 0 || len(document.Spec.Destinations) > 64 {
		return errors.New("policy destinations are invalid")
	}
	seen := make(map[string]struct{}, len(document.Spec.Destinations))
	for index := range document.Spec.Destinations {
		destination := &document.Spec.Destinations[index]
		hostname, err := NormalizeHostname(destination.Hostname)
		if err != nil || destination.Port != 443 {
			return errors.New("policy destination is invalid")
		}
		key := net.JoinHostPort(hostname, strconv.Itoa(destination.Port))
		if _, exists := seen[key]; exists {
			return errors.New("policy destination is duplicated")
		}
		seen[key] = struct{}{}
	}
	sort.Slice(document.Spec.Destinations, func(left, right int) bool {
		return document.Spec.Destinations[left].Hostname < document.Spec.Destinations[right].Hostname
	})
	if len(document.Spec.Profiles) > 1 {
		return errors.New("policy profiles are invalid")
	}
	for _, profile := range document.Spec.Profiles {
		if profile.Name != STTProfileName || profile.Workload != STTWorkload || profile.Operation != STTOperation ||
			len(profile.Destinations) != 1 || profile.Destinations[0] != (Destination{Hostname: "api.openai.com", Port: 443}) {
			return errors.New("policy profile is not registered")
		}
		if _, exists := seen["api.openai.com:443"]; !exists {
			return errors.New("policy profile destination is not registered")
		}
	}
	return nil
}

// canonicalJSON serializes validated policy fields in a stable digest form.
// canonicalJSON сериализует проверенные поля policy в устойчивой форме для дайджеста.
func canonicalJSON(document Document) ([]byte, error) {
	destinations := make([]map[string]any, 0, len(document.Spec.Destinations))
	for _, destination := range document.Spec.Destinations {
		destinations = append(destinations, map[string]any{"hostname": destination.Hostname, "port": destination.Port})
	}
	canonical := map[string]any{
		"apiVersion": document.APIVersion,
		"kind":       document.Kind,
		"metadata": map[string]any{
			"name": document.Metadata.Name, "revision": document.Metadata.Revision,
		},
		"spec": map[string]any{
			"destinations": destinations,
			"dns": map[string]any{
				"maximumCacheEntries":      document.Spec.DNS.MaximumCacheEntries,
				"maximumCnameDepth":        document.Spec.DNS.MaximumCNAMEDepth,
				"maximumMessageBytes":      document.Spec.DNS.MaximumMessageBytes,
				"maximumQueries":           document.Spec.DNS.MaximumQueries,
				"maximumRecords":           document.Spec.DNS.MaximumRecords,
				"maximumTTLSeconds":        document.Spec.DNS.MaximumTTLSeconds,
				"minimumTTLSeconds":        document.Spec.DNS.MinimumTTLSeconds,
				"queryTimeoutMilliseconds": document.Spec.DNS.QueryTimeoutMilliseconds,
			},
			"limits": map[string]any{
				"clientHelloTimeoutMilliseconds": document.Spec.Limits.ClientHelloTimeoutMilliseconds,
				"dialTimeoutMilliseconds":        document.Spec.Limits.DialTimeoutMilliseconds,
				"headerTimeoutMilliseconds":      document.Spec.Limits.HeaderTimeoutMilliseconds,
				"idleTimeoutMilliseconds":        document.Spec.Limits.IdleTimeoutMilliseconds,
				"maximumClientHelloBytes":        document.Spec.Limits.MaximumClientHelloBytes,
				"maximumConnections":             document.Spec.Limits.MaximumConnections,
				"maximumConnectionsPerSource":    document.Spec.Limits.MaximumConnectionsPerSource,
				"maximumHeaderBytes":             document.Spec.Limits.MaximumHeaderBytes,
				"shutdownTimeoutMilliseconds":    document.Spec.Limits.ShutdownTimeoutMilliseconds,
				"writeTimeoutMilliseconds":       document.Spec.Limits.WriteTimeoutMilliseconds,
			},
		},
	}
	if len(document.Spec.Profiles) > 0 {
		profiles := make([]map[string]any, 0, len(document.Spec.Profiles))
		for _, profile := range document.Spec.Profiles {
			profiles = append(profiles, map[string]any{
				"name": profile.Name, "workload": profile.Workload, "operation": profile.Operation,
				"destinations": []map[string]any{{"hostname": profile.Destinations[0].Hostname, "port": profile.Destinations[0].Port}},
			})
		}
		canonical["spec"].(map[string]any)["profiles"] = profiles
	}
	return json.Marshal(canonical)
}

// validRevision accepts a bounded ASCII revision without whitespace or separators beyond the registry.
// validRevision принимает ограниченную ASCII-ревизию без пробелов и лишних разделителей.
func validRevision(value string) bool {
	if len(value) < 3 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '.' && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

// boundedMilliseconds checks an inclusive timeout range.
// boundedMilliseconds проверяет включительный диапазон таймаута.
func boundedMilliseconds(value, minimum, maximum int) bool {
	return value >= minimum && value <= maximum
}

// requireJSONEOF rejects a second JSON value or trailing non-whitespace data.
// requireJSONEOF отклоняет второе значение JSON и лишние данные после документа.
func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("policy contains trailing JSON data")
	}
	return nil
}

// rejectDuplicateFields scans nested JSON objects before typed decoding.
// rejectDuplicateFields проверяет вложенные объекты JSON до типизированного разбора.
func rejectDuplicateFields(value []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	if err := inspectJSONValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

// RejectDuplicateFields exposes the strict JSON duplicate-field check to mail projection.
// RejectDuplicateFields предоставляет строгую проверку полей JSON для mail projection.
func RejectDuplicateFields(value []byte) error { return rejectDuplicateFields(value) }

// inspectJSONValue recursively rejects repeated object keys and malformed delimiters.
// inspectJSONValue рекурсивно отклоняет повторные ключи и неверные разделители.
func inspectJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return errors.New("policy JSON token is invalid")
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return errors.New("policy JSON object is invalid")
			}
			key, keyOK := keyToken.(string)
			if !keyOK {
				return errors.New("policy JSON object key is invalid")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("policy contains duplicate field %q", key)
			}
			seen[key] = struct{}{}
			if err := inspectJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim('}') {
			return errors.New("policy JSON object is incomplete")
		}
	case '[':
		for decoder.More() {
			if err := inspectJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim(']') {
			return errors.New("policy JSON array is incomplete")
		}
	default:
		return errors.New("policy JSON delimiter is invalid")
	}
	return nil
}

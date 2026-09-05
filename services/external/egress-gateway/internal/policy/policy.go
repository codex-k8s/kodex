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
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	APIVersion       = "kodex.io/v1alpha1"
	Kind             = "EgressGatewayPolicy"
	MaximumFileBytes = 64 << 10
	STTProfileName   = "openai-stt"
	STTWorkload      = "stt-tts-service"
	STTOperation     = "openai.transcription"
)

// Document — версионированный machine policy contract.
type Document struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
}

// Metadata задаёт server-owned имя и immutable revision.
type Metadata struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

// Spec задаёт DNS, resource bounds и exact destinations.
type Spec struct {
	DNS          DNSConfig     `json:"dns"`
	Limits       Limits        `json:"limits"`
	Destinations []Destination `json:"destinations"`
	Profiles     []Profile     `json:"profiles,omitempty"`
}

// Profile связывает отдельный listener с закрытым workload/operation набором.
type Profile struct {
	Name         string        `json:"name"`
	Workload     string        `json:"workload"`
	Operation    string        `json:"operation"`
	Destinations []Destination `json:"destinations"`
}

// DNSConfig ограничивает server-owned resolver и cache.
type DNSConfig struct {
	MinimumTTLSeconds        int `json:"minimumTTLSeconds"`
	MaximumTTLSeconds        int `json:"maximumTTLSeconds"`
	MaximumCacheEntries      int `json:"maximumCacheEntries"`
	MaximumQueries           int `json:"maximumQueries"`
	MaximumCNAMEDepth        int `json:"maximumCnameDepth"`
	MaximumRecords           int `json:"maximumRecords"`
	MaximumMessageBytes      int `json:"maximumMessageBytes"`
	QueryTimeoutMilliseconds int `json:"queryTimeoutMilliseconds"`
}

// Limits ограничивает CONNECT, ClientHello, dial, tunnel и shutdown.
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

// Destination — exact normalized FQDN и обязательный TCP/443.
type Destination struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
}

// Active — проверенный immutable policy snapshot.
type Active struct {
	document Document
	digest   string
	allowed  map[string]struct{}
	profile  *Profile
}

// LoadFile bounded-читает, строго валидирует и сверяет revision/digest.
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

// Load строго разбирает policy bytes и создаёт ACTIVE snapshot.
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

// DigestFile возвращает canonical digest policy через тот же parser, что runtime.
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

// Revision возвращает активную immutable revision.
func (active *Active) Revision() string { return active.document.Metadata.Revision }

// Digest возвращает canonical SHA-256 в hex без префикса.
func (active *Active) Digest() string { return active.digest }

// DNS возвращает проверенные DNS bounds.
func (active *Active) DNS() DNSConfig { return active.document.Spec.DNS }

// Limits возвращает проверенные runtime bounds.
func (active *Active) Limits() Limits { return active.document.Spec.Limits }

// Destinations возвращает копию exact allowlist.
func (active *Active) Destinations() []Destination {
	destinations := active.document.Spec.Destinations
	if active.profile != nil {
		destinations = active.profile.Destinations
	}
	result := make([]Destination, len(destinations))
	copy(result, destinations)
	return result
}

// ForProfile разрешает только зарегистрированный профиль того же snapshot.
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

// ProfileIdentity возвращает только проверенные серверные идентификаторы.
func (active *Active) ProfileIdentity() (name, workload, operation string) {
	if active.profile != nil {
		return active.profile.Name, active.profile.Workload, active.profile.Operation
	}
	return "default", "", ""
}

// Allows проверяет exact normalized hostname и port.
func (active *Active) Allows(hostname string, port int) bool {
	_, ok := active.allowed[net.JoinHostPort(hostname, strconv.Itoa(port))]
	return ok
}

// NormalizeHostname принимает только уже canonical lowercase ASCII FQDN.
func NormalizeHostname(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "*@/\\[]:%") || len(value) > 254 {
		return "", errors.New("hostname is invalid")
	}
	if value != strings.ToLower(value) || strings.HasSuffix(value, ".") || len(value) > 253 {
		return "", errors.New("hostname is invalid")
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return "", errors.New("IP literal is prohibited")
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return "", errors.New("hostname must be a FQDN")
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("hostname label is invalid")
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", errors.New("hostname must use ASCII LDH labels")
			}
		}
	}
	return value, nil
}

func validate(document *Document) error {
	if document.APIVersion != APIVersion || document.Kind != Kind || document.Metadata.Name != "egress-gateway" {
		return errors.New("policy identity is invalid")
	}
	if !validRevision(document.Metadata.Revision) {
		return errors.New("policy revision is invalid")
	}
	dns := document.Spec.DNS
	if dns.MinimumTTLSeconds < 5 || dns.MinimumTTLSeconds > 300 ||
		dns.MaximumTTLSeconds < dns.MinimumTTLSeconds || dns.MaximumTTLSeconds > 3600 ||
		dns.MaximumCacheEntries < 4 || dns.MaximumCacheEntries > 256 ||
		dns.MaximumQueries < 2 || dns.MaximumQueries > 32 ||
		dns.MaximumCNAMEDepth < 1 || dns.MaximumCNAMEDepth > 16 ||
		dns.MaximumRecords < 4 || dns.MaximumRecords > 256 ||
		dns.MaximumMessageBytes < 512 || dns.MaximumMessageBytes > 64<<10 ||
		dns.QueryTimeoutMilliseconds < 100 || dns.QueryTimeoutMilliseconds > 10_000 {
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

func boundedMilliseconds(value, minimum, maximum int) bool {
	return value >= minimum && value <= maximum
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("policy contains trailing JSON data")
	}
	return nil
}

func rejectDuplicateFields(value []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	if err := inspectJSONValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

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

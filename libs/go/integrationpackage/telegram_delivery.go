package integrationpackage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// telegramReferencePattern admits only bounded owner references before constructing a protected relative route.
// telegramReferencePattern допускает лишь ограниченные owner references до построения защищённого относительного маршрута.
var telegramReferencePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// TelegramDeliverySnapshot pins one server-created event and destination; it contains no token, caller body or external approval claim.
// TelegramDeliverySnapshot закрепляет одно назначенное сервером событие и назначение; token, caller body и внешнего утверждения approval нет.
type TelegramDeliverySnapshot struct {
	SubscriptionRef       string            `json:"subscription_ref"`
	SubscriptionVersion   int64             `json:"subscription_version"`
	ScopeDigest           string            `json:"scope_digest"`
	ProjectRef            string            `json:"project_ref"`
	RunRef                string            `json:"run_ref"`
	SourceRunVersion      int64             `json:"source_run_version"`
	GateRef               string            `json:"gate_ref"`
	GateVersion           int64             `json:"gate_version"`
	EventType             string            `json:"event_type"`
	TemplateKey           string            `json:"template_key"`
	ConnectionVersion     int64             `json:"connection_version"`
	CredentialRevisionRef string            `json:"credential_revision_ref"`
	DefinitionDigest      string            `json:"definition_digest"`
	PublicOrigin          string            `json:"public_origin"`
	Configuration         map[string]string `json:"configuration"`
	ExpiresAt             time.Time         `json:"expires_at"`
}

// ValidateTelegramPublicOrigin accepts one canonical HTTPS origin from deployment configuration; paths, userinfo and caller-host aliases are not accepted.
// ValidateTelegramPublicOrigin принимает один канонический HTTPS origin из настройки deployment; paths, userinfo и caller-host aliases не допускаются.
func ValidateTelegramPublicOrigin(origin string) error {
	parsed, err := url.Parse(origin)
	if err != nil || len(origin) > 256 || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		parsed.Port() != "" || !validHostname(parsed.Hostname()) || parsed.Host != parsed.Hostname() || parsed.String() != origin {
		return errTelegramConfiguration
	}
	return nil
}

// TelegramDeliveryDigest validates the complete immutable input and hashes its canonical JSON; the digest identifies input and never grants send permission.
// TelegramDeliveryDigest проверяет полный неизменяемый ввод и хеширует его канонический JSON; digest именует ввод и не выдаёт send permission.
func TelegramDeliveryDigest(snapshot TelegramDeliverySnapshot) (string, error) {
	for _, ref := range []string{snapshot.SubscriptionRef, snapshot.ProjectRef, snapshot.RunRef, snapshot.CredentialRevisionRef} {
		if !telegramReferencePattern.MatchString(ref) {
			return "", errTelegramConfiguration
		}
	}
	if snapshot.SubscriptionVersion < 1 || snapshot.ConnectionVersion < 1 || snapshot.SourceRunVersion < 1 || !telegramDigest(snapshot.ScopeDigest) ||
		!telegramDigest(snapshot.DefinitionDigest) || snapshot.ExpiresAt.IsZero() || snapshot.ExpiresAt.Location() != time.UTC ||
		ValidateTelegramPublicOrigin(snapshot.PublicOrigin) != nil {
		return "", errTelegramConfiguration
	}
	if _, err := ParseTelegramConfiguration(snapshot.Configuration); err != nil {
		return "", err
	}
	expected := map[string]string{"FINISHED": "TELEGRAM_FINISHED_V1", "FAILED": "TELEGRAM_FAILED_V1", "HUMAN_GATE": "TELEGRAM_HUMAN_GATE_V1"}
	if expected[snapshot.EventType] == "" || snapshot.TemplateKey != expected[snapshot.EventType] {
		return "", errTelegramConfiguration
	}
	if snapshot.EventType == "HUMAN_GATE" {
		if !telegramReferencePattern.MatchString(snapshot.GateRef) || snapshot.GateVersion < 1 {
			return "", errTelegramConfiguration
		}
	} else if snapshot.GateRef != "" || snapshot.GateVersion != 0 {
		return "", errTelegramConfiguration
	}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) > 8192 {
		return "", errTelegramConfiguration
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// telegramDigest confines immutable hash fields to the one lowercase SHA-256 spelling.
// telegramDigest ограничивает immutable hash fields одним lowercase SHA-256 написанием.
func telegramDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == sha256.Size
}

// TelegramNotificationText renders only a closed status and protected Run URL; project names, prompts, summaries and arbitrary text cannot enter it.
// TelegramNotificationText выводит лишь закрытый статус и защищённый Run URL; project names, prompts, summaries и произвольный текст в него не попадают.
func TelegramNotificationText(snapshot TelegramDeliverySnapshot, locale string) (string, error) {
	if _, err := TelegramDeliveryDigest(snapshot); err != nil {
		return "", err
	}
	texts := map[string]string{"FINISHED": "Запуск завершён.", "FAILED": "Запуск завершился с ошибкой.", "HUMAN_GATE": "Запуск ожидает решения владельца."}
	if locale == "en" {
		texts = map[string]string{"FINISHED": "Run finished.", "FAILED": "Run failed.", "HUMAN_GATE": "Run awaits an owner decision."}
	} else if locale != "ru" {
		return "", errTelegramConfiguration
	}
	return texts[snapshot.EventType] + "\n" + snapshot.PublicOrigin + "/projects/" + snapshot.ProjectRef + "/runs/" + snapshot.RunRef, nil
}

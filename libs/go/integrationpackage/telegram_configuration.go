package integrationpackage

import (
	"errors"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

const (
	// AdapterTelegram identifies the closed outbound interaction adapter; it does not register an agent-callable HTTP sender.
	// AdapterTelegram задаёт закрытый исходящий interaction adapter и не регистрирует вызываемый агентом HTTP sender.
	AdapterTelegram AdapterKey = "TELEGRAM_INTERACTION"
	// TelegramAPIOrigin fixes the only provider origin; a connection cannot choose another host or a local Bot API server.
	// TelegramAPIOrigin фиксирует единственный provider origin; подключение не выбирает иной host либо локальный Bot API server.
	TelegramAPIOrigin = "https://api.telegram.org"
	// TelegramNoTopic explicitly selects chat-level delivery without inventing or creating a forum topic.
	// TelegramNoTopic явно выбирает доставку в chat без выдумывания либо создания forum topic.
	TelegramNoTopic = "NONE"
	// TelegramMaximumIdentifier follows the provider's documented signed 52-bit coordinate bound.
	// TelegramMaximumIdentifier соответствует документированной провайдером границе signed 52-bit координат.
	TelegramMaximumIdentifier int64 = 1<<52 - 1
)

// errTelegramConfiguration rejects malformed or broader connection scopes without returning caller values or credentials.
// errTelegramConfiguration отклоняет неверные либо расширенные connection scopes без выдачи caller values и credentials.
var errTelegramConfiguration = errors.New("telegram connection configuration is invalid")

// TelegramQuietHours stores an explicitly enabled local wall-clock window and a named time zone; disabled is the default.
// TelegramQuietHours хранит явно включённое окно местного времени и именованную time zone; по умолчанию окно выключено.
type TelegramQuietHours struct {
	Enabled     bool
	StartMinute int
	EndMinute   int
	Timezone    string
}

// TelegramConfiguration is a canonical fixed-origin service-bot/chat/topic boundary with finite queue, rate and delivery-expiry limits.
// TelegramConfiguration — каноническая граница fixed-origin service-bot/chat/topic с конечными пределами очереди, частоты и срока доставки.
type TelegramConfiguration struct {
	BotID              int64
	ChatID             int64
	TopicID            int64
	Quiet              TelegramQuietHours
	QueueLimit         int
	RatePerMinute      int
	DeliveryTTLSeconds int
}

// ParseTelegramConfiguration rejects aliases, zero/unsafe identifiers, unknown keys and partial quiet windows before a connection or subscription can become executable.
// ParseTelegramConfiguration отклоняет aliases, нулевые/небезопасные identifiers, неизвестные ключи и неполные quiet windows до допуска подключения либо подписки к исполнению.
func ParseTelegramConfiguration(configuration map[string]string) (TelegramConfiguration, error) {
	allowed := map[string]bool{"base_url": true, "bot_id": true, "chat_id": true, "topic": true, "quiet_enabled": true, "quiet_start": true, "quiet_end": true, "quiet_timezone": true, "queue_limit": true, "rate_per_minute": true, "delivery_ttl_seconds": true}
	for key := range configuration {
		if !allowed[key] {
			return TelegramConfiguration{}, errTelegramConfiguration
		}
	}
	if configuration["base_url"] != TelegramAPIOrigin {
		return TelegramConfiguration{}, errTelegramConfiguration
	}
	bot, err := telegramCanonicalInteger(configuration["bot_id"], 1, TelegramMaximumIdentifier)
	if err != nil {
		return TelegramConfiguration{}, err
	}
	chat, err := telegramCanonicalInteger(configuration["chat_id"], -TelegramMaximumIdentifier, TelegramMaximumIdentifier)
	if err != nil || chat == 0 {
		return TelegramConfiguration{}, errTelegramConfiguration
	}
	var topic int64
	if configuration["topic"] != TelegramNoTopic {
		topic, err = telegramCanonicalInteger(configuration["topic"], 1, 1<<31-1)
		if err != nil {
			return TelegramConfiguration{}, err
		}
	}
	result := TelegramConfiguration{BotID: bot, ChatID: chat, TopicID: topic, QueueLimit: 64, RatePerMinute: 6, DeliveryTTLSeconds: 3600}
	for _, bound := range []struct {
		key              string
		minimum, maximum int64
		target           *int
	}{
		{key: "queue_limit", minimum: 1, maximum: 1000, target: &result.QueueLimit},
		{key: "rate_per_minute", minimum: 1, maximum: 20, target: &result.RatePerMinute},
		{key: "delivery_ttl_seconds", minimum: 60, maximum: 86400, target: &result.DeliveryTTLSeconds},
	} {
		if value, exists := configuration[bound.key]; exists {
			number, err := telegramCanonicalInteger(value, bound.minimum, bound.maximum)
			if err != nil {
				return TelegramConfiguration{}, err
			}
			*bound.target = int(number)
		}
	}
	quiet, exists := configuration["quiet_enabled"]
	if exists && quiet != "true" && quiet != "false" {
		return TelegramConfiguration{}, errTelegramConfiguration
	}
	result.Quiet.Enabled = quiet == "true"
	start, end, zone := configuration["quiet_start"], configuration["quiet_end"], configuration["quiet_timezone"]
	if result.Quiet.Enabled || start != "" || end != "" || zone != "" {
		result.Quiet.StartMinute, err = telegramWallMinute(start)
		if err != nil {
			return TelegramConfiguration{}, err
		}
		result.Quiet.EndMinute, err = telegramWallMinute(end)
		if err != nil || result.Quiet.StartMinute == result.Quiet.EndMinute || zone == "" || zone == "Local" || len(zone) > 80 || strings.ContainsAny(zone, "\x00\r\n\\") || strings.Contains(zone, "..") {
			return TelegramConfiguration{}, errTelegramConfiguration
		}
		if _, err := time.LoadLocation(zone); err != nil {
			return TelegramConfiguration{}, errTelegramConfiguration
		}
		result.Quiet.Timezone = zone
	}
	return result, nil
}

// telegramCanonicalInteger admits one decimal spelling within an explicit range; whitespace, leading zeroes, plus signs and exponent forms are not aliases.
// telegramCanonicalInteger допускает одно десятичное написание в явном диапазоне; пробелы, ведущие нули, plus и exponent forms не считаются aliases.
func telegramCanonicalInteger(value string, minimum, maximum int64) (int64, error) {
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < minimum || number > maximum || strconv.FormatInt(number, 10) != value {
		return 0, errTelegramConfiguration
	}
	return number, nil
}

// telegramWallMinute accepts exact HH:MM in a 24-hour clock without inferring a locale or the server's local time zone.
// telegramWallMinute принимает точное HH:MM по 24-hour clock без вывода locale либо местной server time zone.
func telegramWallMinute(value string) (int, error) {
	if len(value) != 5 || value[2] != ':' {
		return 0, errTelegramConfiguration
	}
	for _, position := range []int{0, 1, 3, 4} {
		if value[position] < '0' || value[position] > '9' {
			return 0, errTelegramConfiguration
		}
	}
	hour, _ := strconv.Atoi(value[:2])
	minute, _ := strconv.Atoi(value[3:])
	if hour > 23 || minute > 59 {
		return 0, errTelegramConfiguration
	}
	return hour*60 + minute, nil
}

// telegramQuietAt evaluates the configured minute interval at a real instant; a wrapped interval crosses midnight and an absent window never blocks work.
// telegramQuietAt проверяет заданный минутный интервал в реальный момент; обёрнутый интервал пересекает полночь, отсутствие окна не блокирует работу.
func (quiet TelegramQuietHours) telegramQuietAt(instant time.Time, location *time.Location) bool {
	if !quiet.Enabled {
		return false
	}
	local := instant.In(location)
	minute := local.Hour()*60 + local.Minute()
	if quiet.StartMinute < quiet.EndMinute {
		return minute >= quiet.StartMinute && minute < quiet.EndMinute
	}
	return minute >= quiet.StartMinute || minute < quiet.EndMinute
}

// NextAllowedTelegramDelivery returns the earliest minute outside an enabled quiet interval and preserves DST folds/gaps by advancing real UTC instants, bounded to 49 hours.
// NextAllowedTelegramDelivery возвращает ближайшую минуту вне включённого quiet interval и сохраняет DST folds/gaps продвижением реальных UTC моментов с пределом49 часов.
// The caller must still check subscription expiry, current scope and rate admission before an effect; this calculation does not grant delivery authority.
// Вызывающий по-прежнему проверяет expiry подписки, текущую scope и rate admission перед эффектом; расчёт не выдаёт права доставки.
func NextAllowedTelegramDelivery(configuration TelegramConfiguration, now time.Time) (time.Time, error) {
	if !configuration.Quiet.Enabled {
		return now.UTC(), nil
	}
	if configuration.Quiet.StartMinute < 0 || configuration.Quiet.StartMinute >= 1440 || configuration.Quiet.EndMinute < 0 || configuration.Quiet.EndMinute >= 1440 || configuration.Quiet.StartMinute == configuration.Quiet.EndMinute || configuration.Quiet.Timezone == "" || configuration.Quiet.Timezone == "Local" {
		return time.Time{}, errTelegramConfiguration
	}
	location, err := time.LoadLocation(configuration.Quiet.Timezone)
	if err != nil {
		return time.Time{}, errTelegramConfiguration
	}
	if !configuration.Quiet.telegramQuietAt(now, location) {
		return now.UTC(), nil
	}
	next := now.UTC().Truncate(time.Minute)
	for minute := 0; minute < 49*60; minute++ {
		next = next.Add(time.Minute)
		if !configuration.Quiet.telegramQuietAt(next, location) {
			return next, nil
		}
	}
	return time.Time{}, errTelegramConfiguration
}

// NormalizeTelegramEvents accepts a nonempty unique subset of the three server template events and returns one stable ordering for scope and intent digests.
// NormalizeTelegramEvents принимает непустое уникальное подмножество трёх server template events и возвращает единый порядок для scope и intent digests.
func NormalizeTelegramEvents(events []string) ([]string, error) {
	if len(events) < 1 || len(events) > 3 {
		return nil, errTelegramConfiguration
	}
	selected := map[string]bool{}
	for _, event := range events {
		if event != "FINISHED" && event != "FAILED" && event != "HUMAN_GATE" || selected[event] {
			return nil, errTelegramConfiguration
		}
		selected[event] = true
	}
	result := make([]string, 0, len(events))
	for _, event := range []string{"FINISHED", "FAILED", "HUMAN_GATE"} {
		if selected[event] {
			result = append(result, event)
		}
	}
	return result, nil
}

// validateTelegramDefinition keeps the system-only two-operation contract fixed across managed revisions without treating a version string as authorization.
// validateTelegramDefinition сохраняет system-only контракт двух операций между managed revisions, не используя version string как authorization.
func validateTelegramDefinition(definition Package) error {
	if definition.Metadata.Key != "telegram" || definition.Spec.Adapter != string(AdapterTelegram) || definition.Spec.AdapterOwner != string(OwnerInteractionGateway) || definition.Spec.ExecutionRoute != string(RouteInteraction) || definition.Spec.HealthCheck.Operation != "telegram.connection.read" || definition.Spec.Credential == nil || definition.Spec.Credential.SecretKey != "bot_token" || definition.Spec.Credential.Kind != "TOKEN" {
		return errTelegramConfiguration
	}
	if len(definition.Spec.NetworkDestinations) != 1 {
		return errTelegramConfiguration
	}
	destination := definition.Spec.NetworkDestinations[0]
	if destination.Source != "STATIC" || destination.Hostname != "api.telegram.org" || destination.Port != 443 || destination.TLS != "REQUIRED" || destination.ConfigurationField != "" {
		return errTelegramConfiguration
	}
	required := map[string]bool{"base_url": false, "bot_id": false, "chat_id": false, "topic": false}
	for _, field := range definition.Spec.ConfigurationFields {
		if _, exists := required[field.Key]; exists {
			required[field.Key] = field.Required
		}
	}
	for _, exists := range required {
		if !exists {
			return errTelegramConfiguration
		}
	}
	for _, capability := range definition.Spec.Capabilities {
		if capability.ResourceScope.Kind != "HTTPS_RESOURCE" || strings.Join(capability.ResourceScope.ConnectionFields, ",") != "base_url,bot_id,chat_id,topic" || capability.CallableByAgent() {
			return errTelegramConfiguration
		}
		switch capability.Operation {
		case "telegram.connection.read":
			if capability.Key != capability.Operation || capability.Risk != "READ" || capability.ApprovalPolicy != "NONE" || len(capability.InputFields) != 0 || !capability.AllowsApprovalPolicy("NONE") {
				return errTelegramConfiguration
			}
		case "telegram.notifications":
			if capability.Key != capability.Operation || capability.Risk != "WRITE" || capability.ApprovalPolicy != "HUMAN_SCOPED" || !capability.AllowsApprovalPolicy("HUMAN_SCOPED") || capability.AllowsApprovalPolicy("NONE") || capability.AllowsApprovalPolicy("HUMAN_EACH_EFFECT") || len(capability.InputFields) != 3 {
				return errTelegramConfiguration
			}
			fields := map[string]bool{}
			for _, field := range capability.InputFields {
				fields[field.Key] = field.Required && field.Type == "STRING"
			}
			if !fields["project_ref"] || !fields["event_type"] || !fields["template_key"] {
				return errTelegramConfiguration
			}
		default:
			return errTelegramConfiguration
		}
	}
	return nil
}

package integrationpackage

import (
	"maps"
	"testing"
	"time"
)

// telegramConfigurationFixture supplies a declared synthetic bot/chat without a forum topic or enabled quiet hours.
// telegramConfigurationFixture задаёт объявленные synthetic bot/chat без forum topic либо включённых quiet hours.
func telegramConfigurationFixture() map[string]string {
	return map[string]string{"base_url": TelegramAPIOrigin, "bot_id": "123456", "chat_id": "-1001234567890", "topic": TelegramNoTopic}
}

// TestTelegramConfigurationIsClosedAndCanonical rejects endpoint substitution, identifier aliases, unbounded limits and token material in public configuration before runtime use.
// TestTelegramConfigurationIsClosedAndCanonical отклоняет подмену endpoint, identifier aliases, неограниченные пределы и token material в public configuration до runtime использования.
func TestTelegramConfigurationIsClosedAndCanonical(t *testing.T) {
	configuration, err := ParseTelegramConfiguration(telegramConfigurationFixture())
	if err != nil || configuration.BotID != 123456 || configuration.ChatID != -1001234567890 || configuration.TopicID != 0 || configuration.Quiet.Enabled || configuration.QueueLimit != 64 || configuration.RatePerMinute != 6 || configuration.DeliveryTTLSeconds != 3600 {
		t.Fatal("canonical fixed-origin scope or finite defaults changed")
	}
	for _, fixture := range []struct{ key, value string }{
		{"base_url", "https://api.telegram.org/"}, {"base_url", "https://api.telegram.org@foreign.example"}, {"base_url", "https://127.0.0.1"},
		{"bot_id", "0"}, {"bot_id", "0123456"}, {"bot_id", "+123456"}, {"bot_id", "4503599627370496"}, {"bot_id", "123456\n"},
		{"chat_id", "0"}, {"chat_id", "-0100"}, {"chat_id", "1e4"}, {"chat_id", "-4503599627370496"}, {"chat_id", "@mutable_alias"},
		{"topic", ""}, {"topic", "0"}, {"topic", "none"}, {"topic", "2147483648"}, {"topic", "+10"},
		{"queue_limit", "0"}, {"queue_limit", "1001"}, {"queue_limit", "01"}, {"rate_per_minute", "21"}, {"delivery_ttl_seconds", "86401"},
		{"quiet_enabled", "TRUE"}, {"quiet_enabled", "1"}, {"bot_token", "synthetic-not-public"}, {"foreign_endpoint", "https://other.example"},
	} {
		current := telegramConfigurationFixture()
		current[fixture.key] = fixture.value
		if _, err := ParseTelegramConfiguration(current); err == nil {
			t.Errorf("accepted invalid scope field %s", fixture.key)
		}
	}
	valid := telegramConfigurationFixture()
	valid["bot_id"] = "4503599627370495"
	valid["chat_id"] = "-4503599627370495"
	valid["topic"] = "2147483647"
	if _, err := ParseTelegramConfiguration(valid); err != nil {
		t.Fatal("documented provider integer boundary rejected")
	}
}

// TestTelegramQuietHoursNeedExplicitCompleteWindow never infers a time zone or enables an absent window and rejects ambiguous/noncanonical wall times.
// TestTelegramQuietHoursNeedExplicitCompleteWindow не выводит time zone и не включает отсутствующее окно, отклоняя неоднозначные/неканонические wall times.
func TestTelegramQuietHoursNeedExplicitCompleteWindow(t *testing.T) {
	base := telegramConfigurationFixture()
	base["quiet_enabled"] = "true"
	base["quiet_start"] = "22:00"
	base["quiet_end"] = "07:00"
	base["quiet_timezone"] = "Europe/Moscow"
	if _, err := ParseTelegramConfiguration(base); err != nil {
		t.Fatal("explicit complete quiet window rejected")
	}
	for _, patch := range []map[string]string{
		{"quiet_timezone": ""}, {"quiet_timezone": "Local"}, {"quiet_timezone": "../zone"}, {"quiet_timezone": "Unknown/Zone"},
		{"quiet_start": "22:0"}, {"quiet_start": "24:00"}, {"quiet_end": "07:60"}, {"quiet_end": "22:00"}, {"quiet_end": ""},
	} {
		current := maps.Clone(base)
		maps.Copy(current, patch)
		if _, err := ParseTelegramConfiguration(current); err == nil {
			t.Fatal("partial or ambiguous quiet window accepted")
		}
	}
}

// TestTelegramQuietSchedulingUsesRealInstants proves earliest eligible UTC times across midnight, non-hour offsets and DST gaps/folds without guessing a nonexistent wall time.
// TestTelegramQuietSchedulingUsesRealInstants доказывает ближайшие допустимые UTC моменты через полночь, дробные offsets и DST gaps/folds без выдумывания отсутствующего wall time.
func TestTelegramQuietSchedulingUsesRealInstants(t *testing.T) {
	for _, fixture := range []struct{ name, zone, start, end, now, want string }{
		{name: "overnight", zone: "Europe/Moscow", start: "22:00", end: "07:00", now: "2026-11-14T20:15:30Z", want: "2026-11-15T04:00:00Z"},
		{name: "same_day", zone: "UTC", start: "10:00", end: "11:00", now: "2026-11-14T10:15:30Z", want: "2026-11-14T11:00:00Z"},
		{name: "offset", zone: "Asia/Kathmandu", start: "15:00", end: "16:00", now: "2026-11-14T09:20:00Z", want: "2026-11-14T10:15:00Z"},
		{name: "spring_gap", zone: "America/New_York", start: "00:00", end: "02:30", now: "2026-03-08T06:15:00Z", want: "2026-03-08T07:00:00Z"},
		{name: "first_fold", zone: "America/New_York", start: "00:00", end: "01:30", now: "2026-11-01T05:15:00Z", want: "2026-11-01T05:30:00Z"},
		{name: "second_fold", zone: "America/New_York", start: "00:00", end: "01:30", now: "2026-11-01T06:15:00Z", want: "2026-11-01T06:30:00Z"},
		{name: "outside_before_fold", zone: "America/New_York", start: "00:00", end: "01:30", now: "2026-11-01T05:45:23Z", want: "2026-11-01T05:45:23Z"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			raw := telegramConfigurationFixture()
			raw["quiet_enabled"] = "true"
			raw["quiet_start"] = fixture.start
			raw["quiet_end"] = fixture.end
			raw["quiet_timezone"] = fixture.zone
			configuration, err := ParseTelegramConfiguration(raw)
			if err != nil {
				t.Fatal("synthetic quiet configuration rejected")
			}
			instant, _ := time.Parse(time.RFC3339, fixture.now)
			want, _ := time.Parse(time.RFC3339, fixture.want)
			next, err := NextAllowedTelegramDelivery(configuration, instant)
			if err != nil || !next.Equal(want) {
				t.Fatalf("wrong eligible instant: %s", next.Format(time.RFC3339))
			}
			again, err := NextAllowedTelegramDelivery(configuration, next)
			if err != nil || !again.Equal(next) {
				t.Fatal("first eligible instant remained quiet")
			}
		})
	}
	configuration, _ := ParseTelegramConfiguration(telegramConfigurationFixture())
	instant := time.Date(2026, 11, 14, 10, 11, 12, 13, time.UTC)
	if next, err := NextAllowedTelegramDelivery(configuration, instant); err != nil || !next.Equal(instant) {
		t.Fatal("disabled quiet hours altered dispatch time")
	}
}

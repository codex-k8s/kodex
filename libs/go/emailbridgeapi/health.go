package emailbridgeapi

import (
	"encoding/json"
	"errors"
	"strings"
)

// HealthSummaryPrefix identifies the versioned, canonical mail health payload.
// RU: HealthSummaryPrefix обозначает версионированный канонический отчёт о состоянии почты.
const HealthSummaryPrefix = "email-health:v1:"

// HealthNotReadyCode is the closed status code surfaced for unavailable mail transport.
// RU: HealthNotReadyCode — закрытый код состояния недоступного почтового транспорта.
const HealthNotReadyCode = "EMAIL_HEALTH_NOT_READY"

// HealthObservation carries only registered status and protocol readiness
// enums; arbitrary provider text and mailbox data are excluded.
// RU: HealthObservation содержит только зарегистрированные статусы и
// готовность протоколов, не включая произвольный ответ провайдера и данные ящика.
type HealthObservation struct {
	Status    string            `json:"status"`
	Protocols ProtocolReadiness `json:"protocols"`
}

// normalizeHealthReason assigns the legacy default only where permitted and
// rejects reason/status combinations outside the closed readiness model.
// RU: normalizeHealthReason подставляет прежнее значение только где это
// допускается и отклоняет сочетания причины и статуса вне закрытой модели.
func normalizeHealthReason(status string, reason *ProtocolReadinessReason) (*ProtocolReadinessReason, bool) {
	if status != "ready" && status != "not_ready" && status != "not_configured" {
		return nil, false
	}
	value := ProtocolReadinessReasonNone
	if status == "not_ready" {
		value = ProtocolReadinessReasonUnavailable
	}
	if reason != nil {
		value = *reason
	}
	if !value.Valid() || (status == "not_ready") == (value == ProtocolReadinessReasonNone) {
		return nil, false
	}
	return &value, true
}

// HealthSummary validates cross-protocol readiness and produces a canonical
// versioned report without copying raw provider responses.
// RU: HealthSummary проверяет согласованность готовности протоколов и создаёт
// канонический версионированный отчёт без копирования сырого ответа провайдера.
func HealthSummary(status string, protocols *ProtocolReadiness) (string, error) {
	invalid := errors.New("mail health observation invalid")
	if protocols == nil || (status != "ready" && status != "not_ready") {
		return "", invalid
	}
	p := *protocols
	var ok bool
	if p.SmtpReason, ok = normalizeHealthReason(string(p.Smtp), p.SmtpReason); !ok {
		return "", invalid
	}
	if p.ImapReason, ok = normalizeHealthReason(string(p.Imap), p.ImapReason); !ok {
		return "", invalid
	}
	if p.Pop3Reason, ok = normalizeHealthReason(string(p.Pop3), p.Pop3Reason); !ok {
		return "", invalid
	}
	if p.Smtp == ProtocolReadinessSmtpNotConfigured || (p.Imap == ProtocolReadinessImapNotConfigured && p.Pop3 == ProtocolReadinessPop3NotConfigured) {
		return "", invalid
	}
	if status == "ready" && (p.Smtp != ProtocolReadinessSmtpReady || p.Imap != ProtocolReadinessImapReady && p.Pop3 != ProtocolReadinessPop3Ready) {
		return "", invalid
	}
	if status == "not_ready" && p.Smtp != ProtocolReadinessSmtpNotReady && p.Imap != ProtocolReadinessImapNotReady && p.Pop3 != ProtocolReadinessPop3NotReady {
		return "", invalid
	}
	b, err := json.Marshal(HealthObservation{Status: status, Protocols: p})
	return HealthSummaryPrefix + string(b), err
}

// ParseHealthSummary accepts only bounded canonical health reports with known
// fields and values, rejecting altered or legacy-ambiguous payloads.
// RU: ParseHealthSummary принимает только ограниченные канонические отчёты
// с известными полями и значениями, отклоняя изменённые либо неоднозначные данные.
func ParseHealthSummary(summary string) (HealthObservation, error) {
	var result HealthObservation
	invalid := errors.New("mail health observation invalid")
	if len(summary) > 1024 || !strings.HasPrefix(summary, HealthSummaryPrefix) {
		return result, invalid
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(summary, HealthSummaryPrefix)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil {
		return HealthObservation{}, invalid
	}
	canonical, err := HealthSummary(result.Status, &result.Protocols)
	if err != nil || canonical != summary {
		return HealthObservation{}, invalid
	}
	return result, nil
}

// CredentialInvalid reports whether any protocol failed specifically because
// authentication was rejected or managed credentials were unavailable.
// RU: CredentialInvalid сообщает, вызван ли сбой какого-либо протокола
// отказом авторизации либо недоступностью управляемых учётных данных.
func (h HealthObservation) CredentialInvalid() bool {
	for _, reason := range []*ProtocolReadinessReason{h.Protocols.SmtpReason, h.Protocols.ImapReason, h.Protocols.Pop3Reason} {
		if reason != nil && (*reason == ProtocolReadinessReasonAuthRejected || *reason == ProtocolReadinessReasonCredentialUnavailable) {
			return true
		}
	}
	return false
}

// LocalizeHealthSummary maps only registered protocol and reason enums to UI
// messages; it never passes raw provider strings to the localization function.
// RU: LocalizeHealthSummary переводит для интерфейса только зарегистрированные
// протоколы и причины, не передавая произвольные строки провайдера.
func LocalizeHealthSummary(summary string, localize func(string) string) string {
	h, err := ParseHealthSummary(summary)
	if err != nil {
		return localize("INTEGRATION_RESPONSE_INVALID")
	}
	rows := []struct {
		protocol, status string
		reason           *ProtocolReadinessReason
	}{{"SMTP", string(h.Protocols.Smtp), h.Protocols.SmtpReason}, {"IMAP", string(h.Protocols.Imap), h.Protocols.ImapReason}, {"POP3", string(h.Protocols.Pop3), h.Protocols.Pop3Reason}}
	parts := make([]string, 0, 3)
	for _, row := range rows {
		token := "EMAIL_HEALTH_" + strings.ToUpper(row.status)
		if row.status == "not_ready" {
			token = "EMAIL_HEALTH_" + strings.ToUpper(string(*row.reason))
		}
		parts = append(parts, row.protocol+": "+localize(token))
	}
	return strings.Join(parts, "; ")
}

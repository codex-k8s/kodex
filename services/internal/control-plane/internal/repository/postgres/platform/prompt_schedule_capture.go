package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryPromptScheduleCapture embeds version1 capture of supplied prompt values and the exact bound
// published/superseded schedule template with source revision and optional scope metadata. It
// reads within the tenant/schedule owner transaction, without materializing the task.
// queryPromptScheduleCapture встраивает снимок версии1 переданных prompt values и точного
// привязанного опубликованного либо superseded шаблона расписания с исходной ревизией и
// необязательной областью. Читает внутри транзакции организации и расписания, не материализуя
// задачу.
//
//go:embed sql/prompt_schedule_capture.sql
var queryPromptScheduleCapture string

// schedulePromptTemplate stores the immutable captured template ref/digest/content, source
// provenance and raw scope metadata for later schedule materialization. It contains template data,
// not current execution authority.
// schedulePromptTemplate хранит неизменяемые ссылку, дайджест и содержимое захваченного шаблона,
// происхождение и сырые метаданные области для последующей материализации расписания. Содержит
// данные шаблона, а не текущие полномочия исполнения.
type schedulePromptTemplate struct {
	Ref            string          `json:"ref"`
	Digest         string          `json:"digest"`
	Content        string          `json:"content"`
	Source         string          `json:"source"`
	SourceRevision string          `json:"sourceRevision"`
	Scope          json.RawMessage `json:"scope"`
}

// schedulePromptCapture envelopes versioned prompt values and an optional captured template.
// Legacy format0 decodes only the values map; current format1 requires Revision1, and decoding
// does not itself authorize template scope.
// schedulePromptCapture объединяет версионные prompt values и необязательный захваченный шаблон.
// Legacy format0 декодирует только карту values; текущий format1 требует Revision1, само
// декодирование не проверяет полномочия области шаблона.
type schedulePromptCapture struct {
	Revision int                     `json:"revision"`
	Values   map[string]any          `json:"values"`
	Template *schedulePromptTemplate `json:"template"`
}

// captureSchedulePromptTx reads the captured JSON envelope, limits it to262144 bytes and validates
// format1 structure/template digest before returning unchanged bytes. SQL failures are
// Unavailable; it neither renders the template nor commits the caller transaction.
// captureSchedulePromptTx читает JSON снимка, ограничивает его262144 байтами и проверяет структуру
// format1 и дайджест шаблона перед возвратом неизменных байтов. Ошибки SQL дают Unavailable; не
// исполняет шаблон и не завершает транзакцию вызывающего кода.
func (repository *Repository) captureSchedulePromptTx(ctx context.Context, tx pgx.Tx, current scope, scheduleRef string, values []byte) ([]byte, error) {
	var captured string
	if err := tx.QueryRow(ctx, queryPromptScheduleCapture, pgx.StrictNamedArgs{"organization_id": current.organizationID, "schedule_ref": scheduleRef, "values": values}).Scan(&captured); err != nil {
		return nil, errs.ErrUnavailable
	}
	if len(captured) > 262144 {
		return nil, errs.ErrInvalid
	}
	if _, err := decodeSchedulePromptCapture(1, []byte(captured)); err != nil {
		return nil, err
	}
	return []byte(captured), nil
}

// schedulePromptDigest hashes raw prompt input with a format-specific schedule-prompt prefix for
// nonzero formats, preserving the legacy unprefixed format0 digest. It does not validate the
// format or parse input.
// schedulePromptDigest хеширует сырые prompt inputs с префиксом schedule-prompt выбранного
// ненулевого формата, сохраняя legacy дайджест format0 без префикса. Не проверяет допустимость
// формата и не разбирает вход.
func schedulePromptDigest(format int, raw []byte) string {
	if format != 0 {
		raw = append([]byte(fmt.Sprintf("schedule-prompt:%d\n", format)), raw...)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// decodeSchedulePromptCapture decodes a nonnil legacy values map for format0 or a Revision1
// envelope for format1, checking optional template ref length and exact content digest.
// Unsupported/malformed data is Conflict; ordinary JSON decoding permits unknown fields and this
// helper imposes no raw-size or authority check.
// decodeSchedulePromptCapture декодирует непустую legacy-карту values для format0 либо оболочку
// Revision1 для format1, проверяя длину ссылки необязательного шаблона и точный дайджест
// содержимого. Неподдерживаемые или повреждённые данные дают Conflict; обычный JSON допускает
// неизвестные поля, helper не ограничивает сырую длину и не проверяет полномочия.
func decodeSchedulePromptCapture(format int, raw []byte) (schedulePromptCapture, error) {
	var value schedulePromptCapture
	if format == 0 {
		if json.Unmarshal(raw, &value.Values) != nil || value.Values == nil {
			return value, errs.ErrConflict
		}
		return value, nil
	}
	if format != 1 || json.Unmarshal(raw, &value) != nil || value.Revision != 1 || value.Values == nil {
		return value, errs.ErrConflict
	}
	if value.Template != nil {
		digest := sha256.Sum256([]byte(value.Template.Content))
		if value.Template.Ref == "" || len(value.Template.Ref) > 128 || value.Template.Digest != hex.EncodeToString(digest[:]) {
			return value, errs.ErrConflict
		}
	}
	return value, nil
}

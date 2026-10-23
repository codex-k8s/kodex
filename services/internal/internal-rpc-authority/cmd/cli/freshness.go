package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
)

// freshnessStatusSQL embeds the named freshness readback query used to populate policy version, age limit, activation metadata and consumer observations; declaring it performs no read.
// freshnessStatusSQL встраивает именованный запрос чтения свежести для версии политики, предела возраста, метаданных активации и наблюдений потребителей; объявление не выполняет чтение.
//
//go:embed sql/freshness__status.sql
var freshnessStatusSQL string

// freshnessActivateSQL embeds the explicit freshness activation query; runFreshness supplies the expected version and activation identity rather than interpolating them into SQL.
// freshnessActivateSQL встраивает явный запрос активации свежести; runFreshness передаёт ожидаемую версию и идентичность активации параметрами, а не подставляет их в SQL.
//
//go:embed sql/freshness__activate.sql
var freshnessActivateSQL string

// freshnessOptions retains the canonical activation UUID accepted by the activation parser; an empty value is used for nonactivation actions and does not itself authorize policy changes.
// freshnessOptions хранит канонический UUID активации, принятый разборщиком; пустое значение используется для других действий и само не разрешает изменения политики.
type freshnessOptions struct{ activationID string }

// freshnessPolicyStatus is the JSON readback of version, age limit, optional activation identity/time, observation time and raw consumer JSON. readFreshnessStatus validates the supported version/activation combinations; the DTO itself verifies no signature or consumer freshness.
// freshnessPolicyStatus — JSON-представление версии, предела возраста, необязательных идентичности и времени активации, времени наблюдения и исходного JSON потребителей. readFreshnessStatus проверяет поддерживаемые сочетания версии и активации; сама структура не проверяет подпись или свежесть потребителя.
type freshnessPolicyStatus struct {
	ObservedAt        time.Time       `json:"observedAt"`
	Consumers         json.RawMessage `json:"consumers"`
	Version           int64           `json:"version"`
	MaximumAgeSeconds int             `json:"maximumAgeSeconds"`
	ActivationID      *string         `json:"activationId"`
	ActivatedAt       *time.Time      `json:"activatedAt"`
}

// parseFreshnessOptions returns empty options for other actions and requires the exact seven activation arguments, expected version one, staging confirmation and canonical nonzero UUID. Syntax confirmation does not confer database authority.
// parseFreshnessOptions возвращает пустые параметры для других действий и требует точные семь аргументов активации, ожидаемую первую версию, подтверждение staging и канонический ненулевой UUID. Синтаксическое подтверждение не даёт полномочий БД.
func parseFreshnessOptions(action command, args []string) (freshnessOptions, error) {
	if action != commandFreshnessActivate {
		return freshnessOptions{}, nil
	}
	if len(args) != 7 || args[1] != "--expected-version" || args[2] != "1" || args[3] != "--activation-id" || args[5] != "--confirm" || args[6] != "ACTIVATE-STAGING-AUTHORITY-FRESHNESS" {
		return freshnessOptions{}, errors.New("invalid freshness activation arguments")
	}
	id, err := uuid.Parse(args[4])
	if err != nil || id == uuid.Nil || id.String() != args[4] {
		return freshnessOptions{}, errors.New("invalid freshness activation identity")
	}
	return freshnessOptions{activationID: args[4]}, nil
}

// readFreshnessStatus reads six fields with the supplied context and accepts only version one with a three-hundred-second age and absent activation, or version two with a thirty-second age and present activation identity/time. It returns sanitized read/shape errors without validating every consumer entry.
// readFreshnessStatus читает шесть полей с переданным контекстом и принимает только первую версию с возрастом триста секунд без активации либо вторую с возрастом тридцать секунд и присутствующими идентичностью и временем активации. Возвращаются безопасные ошибки чтения или формы без проверки каждой записи потребителя.
func readFreshnessStatus(ctx context.Context, db *sql.DB) (freshnessPolicyStatus, error) {
	var result freshnessPolicyStatus
	if err := db.QueryRowContext(ctx, freshnessStatusSQL).Scan(&result.Version, &result.MaximumAgeSeconds, &result.ActivationID, &result.ActivatedAt, &result.ObservedAt, &result.Consumers); err != nil {
		return result, errors.New("freshness policy readback failed")
	}
	if (result.Version == 1 && (result.MaximumAgeSeconds != 300 || result.ActivationID != nil || result.ActivatedAt != nil)) ||
		(result.Version == 2 && (result.MaximumAgeSeconds != 30 || result.ActivationID == nil || result.ActivatedAt == nil)) ||
		(result.Version != 1 && result.Version != 2) {
		return result, errors.New("freshness policy readback rejected")
	}
	return result, nil
}

// runFreshness applies a sixty-second context budget, optionally executes one version-one activation, and returns an explicit uncertain-outcome error on activation failure without retrying. Watch emits at most forty-five one-second observations; activation performs exact identity/version readback before output. Neither a lost response nor output failure authorizes a new activation intent.
// runFreshness задаёт бюджет контекста шестьдесят секунд, при необходимости выполняет одну активацию первой версии и при её ошибке возвращает явную неопределённость исхода без повтора. Наблюдение выводит не более сорока пяти записей с интервалом секунда; активация проверяет точные идентичность и версию перед выводом. Потерянный ответ или ошибка вывода не разрешают новое намерение активации.
func runFreshness(ctx context.Context, db *sql.DB, action command, options freshnessOptions, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if action == commandFreshnessActivate {
		var version int64
		if err := db.QueryRowContext(ctx, freshnessActivateSQL, int64(1), options.activationID).Scan(&version); err != nil {
			// Потерянный ответ не разрешает новое намерение: caller читает status.
			return errors.New("freshness activation outcome uncertain; read freshness-status before any retry")
		}
		if version != 2 {
			return errors.New("freshness activation result rejected")
		}
	}
	if action == commandFreshnessWatch {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for i := 0; i < 45; i++ {
			state, err := readFreshnessStatus(ctx, db)
			if err != nil {
				return err
			}
			if err := json.NewEncoder(output).Encode(state); err != nil {
				return errors.New("write freshness observation")
			}
			if i == 44 {
				return nil
			}
			select {
			case <-ctx.Done():
				return errors.New("freshness observation timeout")
			case <-ticker.C:
			}
		}
	}
	state, err := readFreshnessStatus(ctx, db)
	if err != nil {
		return err
	}
	if action == commandFreshnessActivate && (state.Version != 2 || state.ActivationID == nil || *state.ActivationID != options.activationID) {
		return errors.New("freshness activation readback mismatch")
	}
	if err := json.NewEncoder(output).Encode(state); err != nil {
		return errors.New("write freshness policy readback")
	}
	return nil
}

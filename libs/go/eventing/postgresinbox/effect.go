package postgresinbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/jackc/pgx/v5"
)

// effectTx exposes only registered effect operations inside the processor-owned database transaction.
// effectTx предоставляет только зарегистрированные операции эффекта внутри транзакции, принадлежащей обработчику.
type effectTx struct {
	ctx        context.Context
	tx         pgx.Tx
	operations map[string]EffectOperation
}

// Call verifies the exact registered operation and one JSON input before executing SQL in the current transaction.
// Call проверяет точную зарегистрированную операцию и одно входное значение JSON перед выполнением SQL в текущей транзакции.
func (effect *effectTx) Call(
	operation EffectOperation,
	input json.RawMessage,
) (json.RawMessage, error) {
	registered, ok := effect.operations[operation.name]
	if !ok || registered.query != operation.query || operation.name == "" {
		return nil, ErrEffectOperationNotAllowed
	}
	input = append(json.RawMessage(nil), input...)
	if !singleJSONValue(input) {
		return nil, ErrInvalidEffectInput
	}
	var output json.RawMessage
	if err := effect.tx.QueryRow(
		effect.ctx,
		registered.query,
		pgx.StrictNamedArgs{"effect_input": input},
	).Scan(&output); err != nil {
		return nil, wrapSafe(errorTextEffectOperation, err)
	}
	return append(json.RawMessage(nil), output...), nil
}

// singleJSONValue accepts one decodable JSON value and rejects trailing data after it.
// singleJSONValue принимает одно декодируемое значение JSON и отклоняет любые данные после него.
func singleJSONValue(value []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(value))
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

// buildEffectOperations builds a private name registry, rejecting empty names, empty queries, and duplicates.
// buildEffectOperations строит приватный реестр имён, отклоняя пустые имена, пустые запросы и дубликаты.
func buildEffectOperations(operations []EffectOperation) (map[string]EffectOperation, error) {
	registered := make(map[string]EffectOperation, len(operations))
	for _, operation := range operations {
		if operation.name == "" || operation.query == "" {
			return nil, ErrInvalidEffectOperation
		}
		if _, duplicate := registered[operation.name]; duplicate {
			return nil, ErrInvalidEffectOperation
		}
		registered[operation.name] = operation
	}
	return registered, nil
}

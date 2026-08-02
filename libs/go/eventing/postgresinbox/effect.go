package postgresinbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/jackc/pgx/v5"
)

type effectTx struct {
	ctx        context.Context
	tx         pgx.Tx
	operations map[string]EffectOperation
}

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

func singleJSONValue(value []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(value))
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

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

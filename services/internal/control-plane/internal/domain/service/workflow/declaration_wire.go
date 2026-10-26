package workflow

import (
	"bytes"
	"encoding/json"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// declarationWire preserves numeric lexemes until bounded exact integer validation; JSON Schema
// integers include integral decimal/exponent spellings, which direct int64 unmarshalling rejects.
// declarationWire сохраняет числовые лексемы до ограниченной точной проверки: JSON Schema integer
// включает целые значения с дробной частью либо экспонентой, отвергаемые прямым int64 unmarshal.
type declarationWire struct {
	Schema  string               `json:"schema"`
	Results []declaredResultWire `json:"results"`
}

// declaredResultWire delays artifact revision conversion while retaining strict field decoding.
// declaredResultWire откладывает преобразование ревизии, сохраняя строгий разбор остальных полей.
type declaredResultWire struct {
	Key        string              `json:"key"`
	Source     *artifactSourceWire `json:"source,omitempty"`
	ReceiptRef string              `json:"receiptRef,omitempty"`
	HeadSHA    string              `json:"headSha,omitempty"`
	Verdict    string              `json:"verdict,omitempty"`
}

// artifactSourceWire keeps the untrusted revision as JSON bytes, without float64 rounding or
// accepting quoted numbers. Shape and owner eligibility remain separate mandatory checks.
// artifactSourceWire хранит недоверенную ревизию в JSON-байтах, без округления float64 и принятия
// чисел в строках. Форма и допуск владельца остаются отдельными обязательными проверками.
type artifactSourceWire struct {
	Kind        string          `json:"kind"`
	ArtifactRef string          `json:"artifactRef,omitempty"`
	Revision    json.RawMessage `json:"revision,omitempty"`
	Digest      string          `json:"digest,omitempty"`
}

// revisionNumberPattern accepts the positive JSON number grammar before exact rational parsing.
// revisionNumberPattern допускает грамматику положительного JSON-числа до точного разбора.
var revisionNumberPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// declaredResult converts only bounded structurally representable values, never verifying access.
// declaredResult преобразует лишь ограниченные структурно представимые значения, не проверяя доступ.
func (wire declaredResultWire) declaredResult() (entity.WorkflowDeclaredResult, bool) {
	result := entity.WorkflowDeclaredResult{Key: wire.Key, ReceiptRef: wire.ReceiptRef, HeadSHA: wire.HeadSHA, Verdict: wire.Verdict}
	if wire.Source == nil {
		return result, true
	}
	result.Source = &entity.WorkflowArtifactSource{Kind: wire.Source.Kind, ArtifactRef: wire.Source.ArtifactRef, Digest: wire.Source.Digest}
	if len(wire.Source.Revision) != 0 {
		revision, ok := exactRevision(wire.Source.Revision)
		if !ok {
			return entity.WorkflowDeclaredResult{}, false
		}
		result.Source.Revision = revision
	}
	return result, true
}

// exactRevision accepts positive safe integers in every JSON numeric spelling without rounding.
// Bounding the lexeme and exponent before big.Rat prevents an attacker-selected allocation scale;
// an exponent outside this range cannot yield a positive safe integer within the input byte bound.
// exactRevision принимает положительные безопасные целые во всех числовых формах JSON без
// округления. Ограничения лексемы и экспоненты до big.Rat исключают произвольный масштаб выделения
// памяти; большая экспонента не даст допустимого целого при установленном бюджете входных байтов.
func exactRevision(raw json.RawMessage) (int64, bool) {
	number := string(bytes.TrimSpace(raw))
	if len(number) > MaximumResultBytes || !revisionNumberPattern.MatchString(number) {
		return 0, false
	}
	if index := strings.IndexAny(number, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(number[index+1:])
		if err != nil || exponent < -MaximumResultBytes-16 || exponent > MaximumResultBytes+16 {
			return 0, false
		}
	}
	value, ok := new(big.Rat).SetString(number)
	if !ok || !value.IsInt() || !value.Num().IsInt64() {
		return 0, false
	}
	revision := value.Num().Int64()
	return revision, revision > 0 && revision <= 9007199254740991
}

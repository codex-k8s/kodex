package integration

import (
	"time"

	"github.com/prometheus/prometheus/promql/parser"
)

// observabilityPromQLBudget limits syntactic work and cumulative selector history, not provider cardinality, memory or CPU; server query limits remain mandatory operational controls.
// observabilityPromQLBudget ограничивает синтаксическую работу и суммарную историю selector, а не provider cardinality, memory либо CPU; server query limits остаются обязательными operational controls.
type observabilityPromQLBudget struct{ nodes, selectors int }

// Visit rejects dynamic/absolute time modifiers, excessive cumulative ranges/offsets, nested or implicit-step subqueries and oversized selector graphs using the official 3.14 parser AST.
// Visit отклоняет dynamic/absolute time modifiers, чрезмерные суммарные ranges/offsets, nested/implicit-step subqueries и большие selector graphs по official 3.14 parser AST.
func (budget *observabilityPromQLBudget) Visit(node parser.Node, path []parser.Node) (parser.Visitor, error) {
	if node == nil {
		return budget, nil
	}
	budget.nodes++
	invalid := &SafeError{Code: providerRequestRejectedCode}
	if budget.nodes > 128 || len(path) > 16 {
		return nil, invalid
	}
	var history time.Duration
	subqueries := 0
	for _, ancestor := range append(append([]parser.Node{}, path...), node) {
		switch value := ancestor.(type) {
		case *parser.MatrixSelector:
			if value.RangeExpr != nil || value.Range <= 0 || value.Range > 24*time.Hour {
				return nil, invalid
			}
			history += value.Range
		case *parser.SubqueryExpr:
			subqueries++
			if subqueries > 1 || value.RangeExpr != nil || value.StepExpr != nil || value.OriginalOffsetExpr != nil || value.Timestamp != nil || value.StartOrEnd != 0 || value.Range <= 0 || value.Range > time.Hour || value.Step < time.Minute || value.Range/value.Step > 120 {
				return nil, invalid
			}
			history += value.Range + observabilityAbsDuration(value.OriginalOffset)
		case *parser.VectorSelector:
			if value.OriginalOffsetExpr != nil || value.Timestamp != nil || value.StartOrEnd != 0 || value.Anchored || value.Smoothed {
				return nil, invalid
			}
			history += observabilityAbsDuration(value.OriginalOffset)
		}
		if history > 24*time.Hour {
			return nil, invalid
		}
	}
	if selector, ok := node.(*parser.VectorSelector); ok {
		budget.selectors++
		if budget.selectors > 8 || len(selector.LabelMatchers) > 16 {
			return nil, invalid
		}
		for _, matcher := range selector.LabelMatchers {
			if len(matcher.Name) > 100 || len(matcher.Value) > 256 {
				return nil, invalid
			}
		}
	}
	return budget, nil
}

// observabilityAbsDuration safely bounds signed selector offsets before absolute-value arithmetic, including the minimum duration overflow case.
// observabilityAbsDuration безопасно ограничивает signed selector offsets до абсолютного значения, включая overflow минимального duration.
func observabilityAbsDuration(value time.Duration) time.Duration {
	if value > 24*time.Hour || value < -24*time.Hour {
		return 25 * time.Hour
	}
	if value < 0 {
		return -value
	}
	return value
}

// validateObservabilityPromQL refuses every parser error before using recovered syntax and applies a bounded AST profile without rewriting the user's expression.
// validateObservabilityPromQL отклоняет каждую parser error до использования recovered syntax и применяет bounded AST profile без переписывания выражения пользователя.
func validateObservabilityPromQL(value string, selectorOnly bool) error {
	implementation := parser.NewParser(parser.Options{})
	if selectorOnly {
		matchers, err := implementation.ParseMetricSelector(value)
		if err != nil || len(matchers) == 0 || len(matchers) > 16 {
			return &SafeError{Code: providerRequestRejectedCode}
		}
		for _, matcher := range matchers {
			if len(matcher.Name) > 100 || len(matcher.Value) > 256 {
				return &SafeError{Code: providerRequestRejectedCode}
			}
		}
		return nil
	}
	expression, err := implementation.ParseExpr(value)
	if err != nil || expression == nil {
		return &SafeError{Code: providerRequestRejectedCode}
	}
	return parser.Walk(&observabilityPromQLBudget{}, expression, nil)
}

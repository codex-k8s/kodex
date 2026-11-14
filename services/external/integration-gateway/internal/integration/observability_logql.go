package integration

import (
	"time"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// observabilityLogQLBudget traverses the official syntax AST without rewriting LogQL, bounding nodes, depth, selectors, regular-expression inputs and cumulative historical lookback.
// observabilityLogQLBudget обходит официальный syntax AST без переписывания LogQL, ограничивая nodes, depth, selectors, regex inputs и cumulative historical lookback.
type observabilityLogQLBudget struct {
	nodes, selectors int
	err              error
}

// visit uses the upstream walker's child-pruning contract to maintain a bounded path; formatting templates and experimental variants remain explicitly unsupported because node count cannot bound template allocations or alternate result contracts.
// visit использует upstream child-pruning contract для bounded path; formatting templates и experimental variants явно unsupported, поскольку node count не ограничивает template allocations либо alternative result contracts.
func (budget *observabilityLogQLBudget) visit(node syntax.Expr, depth int, history time.Duration) {
	if budget.err != nil {
		return
	}
	budget.nodes++
	if budget.nodes > 128 || depth > 16 {
		budget.err = &SafeError{Code: providerRequestRejectedCode}
		return
	}
	switch value := node.(type) {
	case *syntax.MatchersExpr:
		budget.selectors++
		if budget.selectors > 8 || len(value.Mts) < 1 || len(value.Mts) > 16 {
			budget.err = &SafeError{Code: providerRequestRejectedCode}
			return
		}
		for _, matcher := range value.Mts {
			if len(matcher.Name) > 100 || len(matcher.Value) > 256 {
				budget.err = &SafeError{Code: providerRequestRejectedCode}
				return
			}
		}
	case *syntax.LogRangeExpr:
		if value.Interval <= 0 || value.Interval > 24*time.Hour {
			budget.err = &SafeError{Code: providerRequestRejectedCode}
			return
		}
		history += value.Interval + observabilityAbsDuration(value.Offset)
		if history > 24*time.Hour {
			budget.err = &SafeError{Code: providerRequestRejectedCode}
			return
		}
	case *syntax.LineFilterExpr:
		if len(value.Match) > 256 {
			budget.err = &SafeError{Code: providerRequestRejectedCode}
			return
		}
	case *syntax.LineParserExpr:
		if len(value.Param) > 256 {
			budget.err = &SafeError{Code: providerRequestRejectedCode}
			return
		}
	case *syntax.LabelReplaceExpr:
		if len(value.Regex) > 256 || len(value.Replacement) > 256 || len(value.Dst) > 100 || len(value.Src) > 100 {
			budget.err = &SafeError{Code: providerRequestRejectedCode}
			return
		}
	case *syntax.LineFmtExpr, *syntax.LabelFmtExpr, *syntax.MultiVariantExpr:
		budget.err = &SafeError{Code: queryProfileUnsupportedCode}
		return
	}
	first := true
	node.Walk(func(child syntax.Expr) bool {
		if first {
			first = false
			return true
		}
		budget.visit(child, depth+1, history)
		return false
	})
}

// validateObservabilityLogQL rejects every parser error before semantic use, then validates the requested logs/metric/selector contract with the pinned official Loki parser and unchanged original expression.
// validateObservabilityLogQL отклоняет каждую parser error до semantic use, затем проверяет logs/metric/selector contract pinned official Loki parser, не изменяя исходное выражение.
func validateObservabilityLogQL(value, kind string) (syntax.Expr, error) {
	if value == "" || len(value) > 4096 {
		return nil, &SafeError{Code: providerRequestRejectedCode}
	}
	expression, err := syntax.ParseExpr(value)
	if err != nil || expression == nil {
		return nil, &SafeError{Code: providerRequestRejectedCode}
	}
	_, sample := expression.(syntax.SampleExpr)
	_, selector := expression.(*syntax.MatchersExpr)
	if kind == "logs" && sample || kind == "metric" && !sample || kind == "selector" && !selector || kind != "logs" && kind != "metric" && kind != "selector" {
		return nil, &SafeError{Code: providerRequestRejectedCode}
	}
	budget := &observabilityLogQLBudget{}
	budget.visit(expression, 0, 0)
	if budget.err != nil {
		return nil, budget.err
	}
	if kind != "metric" && budget.selectors == 0 {
		return nil, &SafeError{Code: providerRequestRejectedCode}
	}
	return expression, nil
}

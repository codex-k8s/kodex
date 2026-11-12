package prompt

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"text/template"
	"text/template/parse"
)

// templateOrigins retains every possible owner-data path of a lexical value; "" is root and "#" is a scalar.
// templateOrigins сохраняет все возможные пути данных владельца для лексического значения; "" — корень, "#" — скаляр.
type templateOrigins []string

// templateScope shares outer binding cells across branches while declarations shadow them locally.
// templateScope разделяет ячейки внешних переменных между ветвями, а объявления перекрывают их локально.
type templateScope struct {
	dot       templateOrigins
	variables map[string]*templateOrigins
}

// templateAnalysis tracks a bounded conservative AST proof, never executing user functions or authorizing data.
// templateAnalysis хранит ограниченное консервативное доказательство AST без исполнения функций и авторизации данных.
type templateAnalysis struct {
	parsed     *template.Template
	allowed    map[string]string
	data       map[string]any
	references map[string]bool
	stack      map[string]bool
	steps      int
	unknown    string
	position   parse.Pos
	execution  bool
}

// analyzeTemplate follows reachable named templates and lexical scopes against the same immutable runtime data.
// analyzeTemplate обходит достижимые именованные шаблоны и лексические области по тем же immutable runtime-данным.
func analyzeTemplate(parsed *template.Template, allowed map[string]string, data map[string]any) *templateAnalysis {
	result := &templateAnalysis{parsed: parsed, allowed: allowed, data: data, references: map[string]bool{}, stack: map[string]bool{}}
	root := templateOrigins{""}
	result.named(parsed.Name(), templateScope{dot: root, variables: map[string]*templateOrigins{"$": &root}}, 1)
	return result
}

// reject preserves the first safe source location without returning template text or values.
// reject сохраняет первую безопасную позицию исходника без возврата текста шаблона и значений.
func (a *templateAnalysis) reject(name string, node parse.Node, execution bool) {
	if a.unknown == "" {
		a.unknown = name
		a.position = node.Position()
		a.execution = execution
	}
}

// named prevents recursive or excessively deep expansion and gives each invocation its own root variable.
// named запрещает рекурсию и чрезмерную глубину, назначая каждому вызову собственную корневую переменную.
func (a *templateAnalysis) named(name string, scope templateScope, cost int) {
	target := a.parsed.Lookup(name)
	if target == nil || target.Tree == nil {
		a.unknown = "template"
		return
	}
	if a.stack[name] || len(a.stack) >= 16 {
		a.reject("template", target.Tree.Root, true)
		return
	}
	a.stack[name] = true
	a.walk(target.Tree.Root, scope, cost)
	delete(a.stack, name)
}

// field resolves dot and variable aliases to qualified catalog paths; descriptor fields require an actual collection item.
// field разрешает dot и псевдонимы в полные пути каталога; поля дескрипторов требуют настоящего элемента коллекции.
func (a *templateAnalysis) field(origins templateOrigins, fields []string, node parse.Node) templateOrigins {
	result := templateOrigins{}
	for _, origin := range origins {
		path := strings.Join(fields, ".")
		if origin == "#" {
			a.reject(path, node, false)
			continue
		}
		if strings.HasSuffix(origin, "[]") {
			if len(fields) != 1 || !allowedTemplateItemField(path) {
				a.reject(path, node, false)
				continue
			}
			a.references[strings.TrimSuffix(origin, "[]")] = true
			result = append(result, "#")
			continue
		}
		if origin != "" {
			path = origin + "." + path
		}
		valid := false
		for candidate := range a.allowed {
			if candidate == path || strings.HasPrefix(candidate, path+".") {
				valid = true
				break
			}
		}
		if !valid {
			a.reject(path, node, false)
			continue
		}
		a.references[path] = true
		result = append(result, path)
	}
	return result
}

// expression propagates dependencies through the closed pure function set without evaluating template code.
// expression переносит зависимости через закрытый набор чистых функций без исполнения кода шаблона.
func (a *templateAnalysis) expression(node parse.Node, scope templateScope, rootArgument bool) templateOrigins {
	if node == nil {
		return templateOrigins{"#"}
	}
	switch n := node.(type) {
	case *parse.DotNode:
		if slices.Contains(scope.dot, "") && !rootArgument {
			a.reject(".", n, false)
		}
		return scope.dot
	case *parse.FieldNode:
		return a.field(scope.dot, n.Ident, n)
	case *parse.VariableNode:
		value, ok := scope.variables[n.Ident[0]]
		if !ok {
			a.reject(n.Ident[0], n, false)
			return nil
		}
		if len(n.Ident) > 1 {
			return a.field(*value, n.Ident[1:], n)
		}
		if slices.Contains(*value, "") && !rootArgument {
			a.reject(n.Ident[0], n, false)
		}
		return *value
	case *parse.ChainNode:
		return a.field(a.expression(n.Node, scope, false), n.Field, n)
	case *parse.PipeNode:
		return a.pipeline(n, scope, rootArgument, false)
	case *parse.CommandNode:
		if len(n.Args) == 1 {
			if _, ok := n.Args[0].(*parse.IdentifierNode); !ok {
				return a.expression(n.Args[0], scope, rootArgument)
			}
		}
		if len(n.Args) == 0 {
			a.reject("command", n, false)
			return nil
		}
		function, ok := n.Args[0].(*parse.IdentifierNode)
		if !ok || !allowedTemplateFunction(function.Ident) {
			a.reject("function", n, false)
			return nil
		}
		var origins templateOrigins
		for _, argument := range n.Args[1:] {
			origins = append(origins, a.expression(argument, scope, false)...)
		}
		if function.Ident == "and" || function.Ident == "or" {
			return origins
		}
		return templateOrigins{"#"}
	case *parse.StringNode, *parse.NumberNode, *parse.BoolNode, *parse.NilNode:
		return templateOrigins{"#"}
	default:
		a.reject("expression", node, false)
		return nil
	}
}

// pipeline applies declarations in the current lexical scope and conservatively unions assignments to outer cells.
// pipeline применяет объявления в текущей области и консервативно объединяет присваивания внешним ячейкам.
func (a *templateAnalysis) pipeline(pipe *parse.PipeNode, scope templateScope, rootArgument, bind bool) templateOrigins {
	if pipe == nil {
		return templateOrigins{"#"}
	}
	var result templateOrigins
	for index, command := range pipe.Cmds {
		previous := result
		result = a.expression(command, scope, rootArgument)
		if index > 0 && len(command.Args) > 0 {
			if function, ok := command.Args[0].(*parse.IdentifierNode); ok && (function.Ident == "and" || function.Ident == "or") {
				result = append(result, previous...)
			}
		}
	}
	if bind {
		for _, declaration := range pipe.Decl {
			name := declaration.Ident[0]
			if pipe.IsAssign {
				previous, ok := scope.variables[name]
				if !ok || name == "$" {
					a.reject(name, declaration, false)
					continue
				}
				merged := append(slices.Clone(*previous), result...)
				slices.Sort(merged)
				merged = slices.Compact(merged)
				*previous = merged
			} else {
				value := slices.Clone(result)
				scope.variables[name] = &value
			}
		}
	}
	return result
}

// rangeSize permits only finite owner collections and bounds total nested work even when loop bodies write nothing.
// rangeSize допускает только конечные коллекции владельца и ограничивает вложенную работу даже при пустом выводе.
func (a *templateAnalysis) rangeSize(origins templateOrigins, node parse.Node) int {
	size := 0
	for _, origin := range origins {
		var value any = a.data
		for _, field := range strings.Split(origin, ".") {
			object, ok := value.(map[string]any)
			if !ok {
				value = nil
				break
			}
			value = object[field]
		}
		if value == nil {
			a.reject(origin, node, true)
			continue
		}
		reflected := reflect.ValueOf(value)
		if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Map && reflected.Kind() != reflect.Array {
			a.reject(origin, node, true)
			continue
		}
		size = max(size, reflected.Len())
	}
	return max(1, size)
}

// walk visits both branches for dependency safety; Go text/template remains the sole renderer and controls actual execution.
// walk обходит обе ветви для безопасного учёта зависимостей; единственным renderer и владельцем исполнения остаётся Go text/template.
func (a *templateAnalysis) walk(node parse.Node, scope templateScope, cost int) {
	if node == nil || (reflect.ValueOf(node).Kind() == reflect.Pointer && reflect.ValueOf(node).IsNil()) || a.unknown != "" {
		return
	}
	a.steps += cost
	if a.steps > 65536 {
		a.reject("execution_budget", node, true)
		return
	}
	switch n := node.(type) {
	case *parse.ListNode:
		for _, child := range n.Nodes {
			a.walk(child, scope, cost)
		}
	case *parse.TextNode, *parse.BreakNode, *parse.ContinueNode:
	case *parse.ActionNode:
		a.pipeline(n.Pipe, scope, len(n.Pipe.Decl) > 0, true)
	case *parse.IfNode:
		a.branch(&n.BranchNode, scope, cost, "if")
	case *parse.WithNode:
		a.branch(&n.BranchNode, scope, cost, "with")
	case *parse.RangeNode:
		a.branch(&n.BranchNode, scope, cost, "range")
	case *parse.TemplateNode:
		value := a.pipeline(n.Pipe, scope, true, true)
		root := slices.Clone(value)
		a.named(n.Name, templateScope{dot: value, variables: map[string]*templateOrigins{"$": &root}}, cost)
	default:
		a.reject("node", node, false)
	}
}

// branch models Go lexical dot/declaration rules while keeping outer assignment possibilities conservative across paths.
// branch моделирует правила dot и объявлений Go, сохраняя консервативное объединение внешних присваиваний по путям.
func (a *templateAnalysis) branch(node *parse.BranchNode, scope templateScope, cost int, kind string) {
	child := templateScope{dot: scope.dot, variables: maps.Clone(scope.variables)}
	value := a.pipeline(node.Pipe, child, false, kind != "range")
	body := templateScope{dot: child.dot, variables: maps.Clone(child.variables)}
	if kind == "with" {
		body.dot = value
	}
	if kind == "range" {
		count := a.rangeSize(value, node)
		if count > 65536 || cost > 65536/count {
			a.reject("execution_budget", node, true)
			return
		}
		item := templateOrigins{}
		for _, origin := range value {
			if origin == "input.values" {
				item = append(item, "#")
			} else {
				item = append(item, origin+"[]")
			}
		}
		body.dot = item
		for index, declaration := range node.Pipe.Decl {
			binding := slices.Clone(item)
			if len(node.Pipe.Decl) == 2 && index == 0 {
				binding = templateOrigins{"#"}
			}
			name := declaration.Ident[0]
			if node.Pipe.IsAssign {
				cell, ok := body.variables[name]
				if !ok || name == "$" {
					a.reject(name, declaration, false)
					continue
				}
				*cell = append(slices.Clone(*cell), binding...)
			} else {
				body.variables[name] = &binding
				child.variables[name] = &binding
			}
		}
		a.walk(node.List, body, cost*count)
	} else {
		a.walk(node.List, body, cost)
	}
	a.walk(node.ElseList, child, cost)
}

// templateVariableReferences returns sorted conservative dependencies including control flow, aliases and named templates.
// templateVariableReferences возвращает отсортированные консервативные зависимости условий, псевдонимов и именованных шаблонов.
func templateVariableReferences(parsed *template.Template) []string {
	analysis := analyzeTemplate(parsed, Catalog(), validationTemplateData())
	result := make([]string, 0, len(analysis.references))
	for name := range analysis.references {
		result = append(result, name)
	}
	slices.Sort(result)
	return result
}

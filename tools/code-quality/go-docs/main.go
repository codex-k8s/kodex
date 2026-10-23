// Package main inventories bilingual documentation of handwritten Go declarations.
// Пакет main составляет реестр двуязычных комментариев собственных Go-объявлений.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// declaration identifies one named declaration without copying its executable body.
// declaration определяет именованное объявление, не копируя его исполняемое тело.
type declaration struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Bilingual bool   `json:"bilingual"`
}

// sourceInventory is the shared source list after generated outputs are excluded.
// sourceInventory хранит общий список исходников после исключения generated output.
type sourceInventory struct {
	Sources []string `json:"sources"`
}

// report records every declaration, retaining uncovered entries rather than hiding them.
// report сохраняет все объявления, включая непокрытые, без их исключения из отчёта.
type report struct {
	Files        int            `json:"files"`
	Total        int            `json:"total"`
	Bilingual    int            `json:"bilingual"`
	Declarations []declaration  `json:"declarations"`
	ByKind       map[string]int `json:"byKind"`
}

// englishWord finds candidate English words; bilingual still requires multiple
// words before the Russian translation and does not prove semantic accuracy.
// englishWord ищет английские слова; bilingual требует несколько слов до русского
// перевода, но не доказывает точность содержания комментария.
var englishWord = regexp.MustCompile(`[A-Za-z]{3,}`)

// bilingual checks language order only; semantic accuracy still requires reading the code.
// bilingual проверяет только порядок языков; точность смысла требует чтения кода.
func bilingual(comment *ast.CommentGroup) bool {
	if comment == nil {
		return false
	}
	english := false
	for _, line := range strings.Split(comment.Text(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cyrillic := strings.ContainsFunc(line, func(r rune) bool { return unicode.In(r, unicode.Cyrillic) })
		if cyrillic {
			return english
		}
		if len(englishWord.FindAllString(line, -1)) >= 2 {
			english = true
		}
	}
	return false
}

// inspectFile records top-level declarations, local named types and interface
// methods in source order; ordinary local variables are not separate entities.
// inspectFile фиксирует верхнеуровневые объявления, локальные именованные типы
// и методы интерфейсов по порядку; обычные локальные переменные не отдельные сущности.
func inspectFile(filename string, source []byte) ([]declaration, error) {
	positions := token.NewFileSet()
	file, err := parser.ParseFile(positions, filename, source, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return nil, fmt.Errorf("parse Go source: %w", err)
	}
	var declarations []declaration
	add := func(kind, name string, at token.Pos, comment *ast.CommentGroup) {
		if name == "_" {
			return
		}
		declarations = append(declarations, declaration{Path: filename, Line: positions.Position(at).Line, Kind: kind, Name: name, Bilingual: bilingual(comment)})
	}
	seenTypes := make(map[*ast.TypeSpec]bool)
	recordType := func(spec *ast.TypeSpec, fallback *ast.CommentGroup) {
		if seenTypes[spec] {
			return
		}
		seenTypes[spec] = true
		comment := spec.Doc
		if comment == nil {
			comment = fallback
		}
		add("type", spec.Name.Name, spec.Pos(), comment)
		if contract, ok := spec.Type.(*ast.InterfaceType); ok {
			for _, method := range contract.Methods.List {
				for _, name := range method.Names {
					add("interface-method", spec.Name.Name+"."+name.Name, name.Pos(), method.Doc)
				}
			}
		}
	}
	for _, node := range file.Decls {
		switch node := node.(type) {
		case *ast.FuncDecl:
			kind, name := "function", node.Name.Name
			if node.Recv != nil {
				kind = "method"
				var receiver ast.Expr = node.Recv.List[0].Type
				if pointer, ok := receiver.(*ast.StarExpr); ok {
					receiver = pointer.X
				}
				if generic, ok := receiver.(*ast.IndexExpr); ok {
					receiver = generic.X
				}
				if generic, ok := receiver.(*ast.IndexListExpr); ok {
					receiver = generic.X
				}
				if ident, ok := receiver.(*ast.Ident); ok {
					name = ident.Name + "." + name
				}
			}
			add(kind, name, node.Pos(), node.Doc)
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					recordType(spec, node.Doc)
				case *ast.ValueSpec:
					comment := spec.Doc
					if comment == nil {
						comment = node.Doc
					}
					for _, name := range spec.Names {
						kind := "variable"
						if node.Tok == token.CONST {
							kind = "constant"
						}
						add(kind, name.Name, name.Pos(), comment)
					}
				}
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if declaration, ok := node.(*ast.GenDecl); ok {
			for _, spec := range declaration.Specs {
				if namedType, ok := spec.(*ast.TypeSpec); ok {
					recordType(namedType, declaration.Doc)
				}
			}
		}
		return true
	})
	sort.SliceStable(declarations, func(i, j int) bool { return declarations[i].Line < declarations[j].Line })
	return declarations, nil
}

// run reads only the explicit inventory and writes a report without changing Go files.
// run читает только явный реестр и записывает отчёт, не изменяя Go-исходники.
func run(root, inventoryPath, outputPath string, requireComplete bool) error {
	input, err := os.ReadFile(inventoryPath)
	if err != nil {
		return fmt.Errorf("read source inventory: %w", err)
	}
	var inventory sourceInventory
	if err := json.Unmarshal(input, &inventory); err != nil {
		return fmt.Errorf("decode source inventory: %w", err)
	}
	result := report{ByKind: make(map[string]int)}
	for _, filename := range inventory.Sources {
		if filepath.Ext(filename) != ".go" {
			continue
		}
		if !filepath.IsLocal(filename) {
			return fmt.Errorf("source inventory contains a non-local path")
		}
		source, err := os.ReadFile(filepath.Join(root, filename))
		if err != nil {
			return fmt.Errorf("read Go source: %w", err)
		}
		declarations, err := inspectFile(filename, source)
		if err != nil {
			return err
		}
		result.Files++
		for _, declaration := range declarations {
			result.Total++
			result.ByKind[declaration.Kind]++
			if declaration.Bilingual {
				result.Bilingual++
			}
			result.Declarations = append(result.Declarations, declaration)
		}
	}
	if result.Total == 0 {
		return fmt.Errorf("Go declaration inventory is empty")
	}
	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode documentation report: %w", err)
	}
	if err := os.WriteFile(outputPath, append(output, '\n'), 0o600); err != nil {
		return fmt.Errorf("write documentation report: %w", err)
	}
	fmt.Printf("{\"files\":%d,\"declarations\":%d,\"bilingual\":%d,\"missing\":%d}\n", result.Files, result.Total, result.Bilingual, result.Total-result.Bilingual)
	if requireComplete && result.Total != result.Bilingual {
		return fmt.Errorf("bilingual Go documentation is incomplete")
	}
	return nil
}

// main exposes a read-only inventory command with explicit input and output paths.
// main предоставляет команду анализа с явными входным реестром и путём отчёта.
func main() {
	root := flag.String("root", ".", "repository root")
	inventory := flag.String("inventory", "", "source inventory JSON path")
	output := flag.String("output", "", "documentation report JSON path")
	check := flag.Bool("check", false, "fail when bilingual documentation is incomplete")
	flag.Parse()
	if err := run(*root, *inventory, *output, *check); err != nil {
		fmt.Fprintln(os.Stderr, "Go documentation inventory failed:", err)
		os.Exit(1)
	}
}

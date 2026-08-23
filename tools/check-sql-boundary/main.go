package main

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	sqlLiteral  = regexp.MustCompile(`(?is)^\s*(?:--[^\n]*\n\s*)*(?:select\b.*\bfrom\b|with\b.*\b(?:select|insert|update|delete)\b|insert\s+into\b|update\s+[a-z_][a-z0-9_.]*\s+set\b|delete\s+from\b|create\s+(?:table|schema|index|function)\b|alter\s+(?:table|role)\b|drop\s+(?:table|schema|index|function)\b|grant\s+.+\s+to\b|revoke\s+.+\s+from\b|set\s+(?:local|session|transaction|role)\b)`)
	queryHeader = regexp.MustCompile(`^-- name: ([a-z0-9_]+) :(one|many|exec)$`)
)

func main() {
	repositoryRoot, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	if len(os.Args) == 2 {
		repositoryRoot, err = filepath.Abs(os.Args[1])
		if err != nil {
			fail(err)
		}
	} else if len(os.Args) > 2 {
		fail(errors.New("usage: check-sql-boundary [repository-root]"))
	}

	violations, err := inspectRepository(repositoryRoot)
	if err != nil {
		fail(err)
	}
	if len(violations) != 0 {
		sort.Strings(violations)
		for _, violation := range violations {
			fmt.Fprintln(os.Stderr, violation)
		}
		os.Exit(1)
	}

	fmt.Println("SQL boundary: PASS")
}

func inspectRepository(repositoryRoot string) ([]string, error) {
	var violations []string
	embeddedSQL := make(map[string]int)
	productionSQL := make(map[string]struct{})
	fileSet := token.NewFileSet()

	for _, root := range []string{"services", "libs", "tools"} {
		err := filepath.WalkDir(filepath.Join(repositoryRoot, root), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if skipDirectory(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}

			switch filepath.Ext(path) {
			case ".go":
				parsed, parseErr := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
				if parseErr != nil {
					return fmt.Errorf("parse %s: %w", relative(repositoryRoot, path), parseErr)
				}
				ast.Inspect(parsed, func(node ast.Node) bool {
					literal, ok := node.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						return true
					}
					value, unquoteErr := strconv.Unquote(literal.Value)
					if unquoteErr == nil && sqlLiteral.MatchString(value) {
						position := fileSet.Position(literal.Pos())
						violations = append(violations, fmt.Sprintf("%s:%d: SQL literal in production Go; move the query to an embedded .sql file", relative(repositoryRoot, path), position.Line))
					}
					return true
				})
				for _, commentGroup := range parsed.Comments {
					for _, comment := range commentGroup.List {
						const directive = "//go:embed "
						if !strings.HasPrefix(comment.Text, directive) {
							continue
						}
						for _, pattern := range strings.Fields(strings.TrimPrefix(comment.Text, directive)) {
							if filepath.Ext(pattern) != ".sql" || strings.ContainsAny(pattern, "*?[]") {
								continue
							}
							target := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(pattern)))
							embeddedSQL[target]++
						}
					}
				}
			case ".sql":
				if isProductionQuery(path) {
					productionSQL[path] = struct{}{}
					if headerErr := validateQueryHeader(path); headerErr != nil {
						violations = append(violations, fmt.Sprintf("%s: %v", relative(repositoryRoot, path), headerErr))
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	for path := range productionSQL {
		if embeddedSQL[path] != 1 {
			violations = append(violations, fmt.Sprintf("%s: production query must be referenced by exactly one explicit //go:embed directive; found %d", relative(repositoryRoot, path), embeddedSQL[path]))
		}
	}
	return violations, nil
}

func validateQueryHeader(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		match := queryHeader.FindStringSubmatch(line)
		if match == nil {
			return errors.New("first content line must be '-- name: <file-name> :one|:many|:exec'")
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if match[1] != name {
			return fmt.Errorf("query name %q does not match file name %q", match[1], name)
		}
		return nil
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return errors.New("query file is empty")
}

func isProductionQuery(path string) bool {
	normalized := filepath.ToSlash(path)
	return strings.Contains(normalized, "/sql/") &&
		!strings.Contains(normalized, "/testdata/")
}

func skipDirectory(name string) bool {
	switch name {
	case ".git", "dist", "generated", "node_modules", "vendor":
		return true
	default:
		return false
	}
}

func relative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

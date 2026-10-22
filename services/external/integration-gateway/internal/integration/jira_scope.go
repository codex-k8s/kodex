package integration

import (
	"strings"
	"unicode"
)

// scopedJiraQuery checks balanced quotes and parentheses under a depth limit, wraps the predicate in the project boundary and keeps top-level ORDER BY outside; Jira still validates the full JQL grammar.
// scopedJiraQuery проверяет баланс кавычек и скобок с ограничением глубины, оборачивает условие границей проекта и оставляет верхнеуровневый ORDER BY снаружи; полную грамматику JQL по-прежнему проверяет Jira.
func scopedJiraQuery(project, query string) (string, bool) {
	depth := 0
	quote := byte(0)
	escaped := false
	// token records a top-level word and its byte offset so the scope wrapper can separate a trailing ORDER BY without rewriting quoted or nested expressions.
	// token хранит верхнеуровневое слово и его байтовую позицию, позволяя отделить завершающий ORDER BY без изменения выражений в кавычках или скобках.
	type token struct {
		word  string
		start int
	}
	var top []token
	for i := 0; i < len(query); i++ {
		c := query[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return "", false
			}
		}
		if depth > 100 {
			return "", false
		}
		if depth == 0 && unicode.IsLetter(rune(c)) {
			start := i
			for i+1 < len(query) && (unicode.IsLetter(rune(query[i+1])) || query[i+1] == '_') {
				i++
			}
			top = append(top, token{strings.ToUpper(query[start : i+1]), start})
		}
	}
	if quote != 0 || depth != 0 {
		return "", false
	}
	where, order := query, ""
	for i := 0; i+1 < len(top); i++ {
		if top[i].word == "ORDER" && top[i+1].word == "BY" {
			between := query[top[i].start+len(top[i].word) : top[i+1].start]
			if strings.TrimSpace(between) == "" {
				where, order = query[:top[i].start], query[top[i].start:]
				break
			}
		}
	}
	if strings.TrimSpace(where) == "" {
		return "", false
	}
	return `project = "` + strings.ReplaceAll(project, `"`, `\"`) + `" AND (` + where + `) ` + order, true
}

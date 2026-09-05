package integration

import (
	"strings"
	"unicode"
)

// Проверяем замкнутость выражения перед добавлением project boundary.
// Грамматику JQL проверяет Jira; сортировка остаётся за внешними скобками.
func scopedJiraQuery(project, query string) (string, bool) {
	depth := 0
	quote := byte(0)
	escaped := false
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

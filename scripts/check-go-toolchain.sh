#!/usr/bin/env bash
set -euo pipefail

security_floor="1.26.5"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
static_only=false

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

version_at_least() {
  local actual="$1"
  local minimum="$2"
  local actual_major actual_minor actual_patch
  local minimum_major minimum_minor minimum_patch

  IFS=. read -r actual_major actual_minor actual_patch <<<"$actual"
  IFS=. read -r minimum_major minimum_minor minimum_patch <<<"$minimum"

  ((10#$actual_major > 10#$minimum_major)) && return 0
  ((10#$actual_major < 10#$minimum_major)) && return 1
  ((10#$actual_minor > 10#$minimum_minor)) && return 0
  ((10#$actual_minor < 10#$minimum_minor)) && return 1
  ((10#$actual_patch >= 10#$minimum_patch))
}

require_file() {
  [[ -f "$repo_root/$1" ]] || fail "отсутствует канонический источник $1"
}

require_line() {
  local path="$1"
  local expected="$2"

  grep -Fqx "$expected" "$repo_root/$path" || fail "$path не содержит ожидаемую строку: $expected"
}

require_count() {
  local path="$1"
  local expected="$2"
  local wanted="$3"
  local actual

  actual="$(grep -Fc "$expected" "$repo_root/$path" || true)"
  [[ "$actual" == "$wanted" ]] || fail "$path содержит '$expected' $actual раз; ожидается $wanted"
}

validate_dockerfile_lexical_boundary() {
  local dockerfile="$1"

  LC_ALL=C awk '
    function lexical_error(message) {
      print message > "/dev/stderr"
      validation_failed = 1
      exit 2
    }

    function contains_unicode_whitespace(value) {
      # Байтовый список соответствует не-ASCII символам unicode.IsSpace в BuildKit v0.29.0.
      return index(value, "\302\205") > 0 || \
        index(value, "\302\240") > 0 || \
        index(value, "\341\232\200") > 0 || \
        index(value, "\342\200\200") > 0 || \
        index(value, "\342\200\201") > 0 || \
        index(value, "\342\200\202") > 0 || \
        index(value, "\342\200\203") > 0 || \
        index(value, "\342\200\204") > 0 || \
        index(value, "\342\200\205") > 0 || \
        index(value, "\342\200\206") > 0 || \
        index(value, "\342\200\207") > 0 || \
        index(value, "\342\200\210") > 0 || \
        index(value, "\342\200\211") > 0 || \
        index(value, "\342\200\212") > 0 || \
        index(value, "\342\200\250") > 0 || \
        index(value, "\342\200\251") > 0 || \
        index(value, "\342\200\257") > 0 || \
        index(value, "\342\201\237") > 0 || \
        index(value, "\343\200\200") > 0
    }

    {
      line = $0
      if (substr(line, length(line), 1) == "\r") {
        line = substr(line, 1, length(line) - 1)
      }

      if (index(line, "\357\273\277") > 0) {
        lexical_error("строка " NR ": UTF-8 BOM в Dockerfile не поддерживается")
      }

      if (contains_unicode_whitespace(line)) {
        lexical_error("строка " NR ": недопустимый Unicode-пробел в Dockerfile")
      }

      for (position = 1; position <= length(line); position++) {
        character = substr(line, position, 1)
        if (character ~ /[[:cntrl:]]/ && character != "\t") {
          lexical_error("строка " NR ": недопустимый управляющий ASCII-байт в Dockerfile")
        }
      }
    }

    END {
      if (validation_failed) {
        exit 2
      }
    }
  ' "$dockerfile"
}

validate_dockerfile_parser_boundary() {
  local dockerfile="$1"

  LC_ALL=C awk '
    function parser_boundary_error(message) {
      print message > "/dev/stderr"
      validation_failed = 1
      exit 2
    }

    function parser_directive_key(value, prefix, body, separator, name, directive_value, key) {
      if (substr(value, 1, length(prefix)) != prefix) {
        return ""
      }

      body = substr(value, length(prefix) + 1)
      sub(/^[ \t]+/, "", body)
      separator = index(body, "=")
      if (separator == 0) {
        return ""
      }

      name = substr(body, 1, separator - 1)
      sub(/[ \t]+$/, "", name)
      if (name !~ /^[A-Za-z][A-Za-z0-9]*$/) {
        return ""
      }

      directive_value = substr(body, separator + 1)
      sub(/^[ \t]+/, "", directive_value)
      sub(/[ \t]+$/, "", directive_value)
      if (directive_value == "") {
        return ""
      }

      key = toupper(name)
      if (key != "SYNTAX" && key != "ESCAPE" && key != "CHECK") {
        return ""
      }
      return key
    }

    BEGIN {
      hash_directives = 1
      slash_directives = 1
      json_candidate = 1
    }

    {
      line = $0
      sub(/\r$/, "", line)

      # BuildKit DetectSyntax удаляет shebang первой строки перед поиском frontend.
      if (NR == 1 && substr(line, 1, 2) == "#!") {
        next
      }

      if (hash_directives) {
        key = parser_directive_key(line, "#")
        if (key == "") {
          hash_directives = 0
        } else if (key == "SYNTAX") {
          parser_boundary_error("внешний Dockerfile syntax frontend не поддерживается")
        }
      }

      if (slash_directives) {
        key = parser_directive_key(line, "//")
        if (key == "") {
          slash_directives = 0
        } else if (key == "SYNTAX") {
          parser_boundary_error("внешний Dockerfile syntax frontend не поддерживается")
        }
      }

      if (json_candidate) {
        json_start = line
        sub(/^[ \t]+/, "", json_start)
        if (json_start == "") {
          next
        }
        # Любой JSON-object здесь не является допустимым встроенным Dockerfile;
        # закрытый отказ покрывает JSON-форму DetectSyntax без собственного JSON parser.
        if (substr(json_start, 1, 1) == "{") {
          parser_boundary_error("внешний Dockerfile syntax frontend не поддерживается")
        }
        json_candidate = 0
      }
    }

    END {
      if (validation_failed) {
        exit 2
      }
    }
  ' "$dockerfile"
}

final_runtime_stage() {
  local dockerfile="$1"

  LC_ALL=C awk '
    function parse_error(message) {
      print message > "/dev/stderr"
      parse_failed = 1
      exit 2
    }

    function has_line_continuation(value, trimmed, position, slash_count) {
      trimmed = value
      sub(/[ \t]+$/, "", trimmed)
      slash_count = 0
      for (position = length(trimmed); position > 0 && substr(trimmed, position, 1) == "\\"; position--) {
        slash_count++
      }
      # BuildKit v0.29.0 продолжает строку только после единственного завершающего escape.
      return slash_count == 1
    }

    function strip_line_continuation(value, trimmed) {
      trimmed = value
      sub(/[ \t]+$/, "", trimmed)
      sub(/\\$/, "", trimmed)
      return trimmed
    }

    function process_instruction(value, start_line, line, instruction, body) {
      line = value
      sub(/^[ \t]+/, "", line)
      if (index(line, "<<") > 0) {
        parse_error("Dockerfile heredoc не поддерживается проверкой final runtime stage")
      }
      if (!match(line, /^[^ \t]+/)) {
        return
      }

      instruction = toupper(substr(line, 1, RLENGTH))
      if (instruction != "FROM") {
        return
      }

      body = substr(line, RLENGTH + 1)
      sub(/^[ \t]+/, "", body)
      sub(/[ \t]+$/, "", body)
      if (body == "") {
        parse_error("не удалось однозначно разобрать FROM-инструкцию")
      }
      from_line = start_line
      from_body = body
      found_from = 1
    }

    {
      line = $0
      sub(/\r$/, "", line)
      directive = line
      sub(/^[ \t]+/, "", directive)
      if (toupper(directive) ~ /^#[ \t]*ESCAPE[ \t]*=/) {
        parse_error("Dockerfile parser directive escape не поддерживается проверкой final runtime stage")
      }
      if (line ~ /^[ \t]*($|#)/) {
        next
      }
      if (logical == "") {
        logical_start = NR
      }
      if (has_line_continuation(line)) {
        logical = logical strip_line_continuation(line)
        next
      }
      logical = logical line
      process_instruction(logical, logical_start)
      logical = ""
    }

    END {
      if (parse_failed) {
        exit 2
      }
      if (logical != "") {
        print "незавершённая логическая Dockerfile-инструкция" > "/dev/stderr"
        exit 2
      }
      if (!found_from) {
        print "Dockerfile не содержит runtime stage" > "/dev/stderr"
        exit 2
      }
      print from_line "\t" from_body
    }
  ' "$dockerfile"
}

effective_final_stage_gotoolchain() {
  local dockerfile="$1"
  local from_line="$2"

  tail -n +"$from_line" "$dockerfile" | LC_ALL=C awk '
    function parse_error(message) {
      print message > "/dev/stderr"
      parse_failed = 1
      exit 2
    }

    function has_line_continuation(value, trimmed, position, slash_count) {
      trimmed = value
      sub(/[ \t]+$/, "", trimmed)
      slash_count = 0
      for (position = length(trimmed); position > 0 && substr(trimmed, position, 1) == "\\"; position--) {
        slash_count++
      }
      # BuildKit v0.29.0 продолжает строку только после единственного завершающего escape.
      return slash_count == 1
    }

    function strip_line_continuation(value, trimmed) {
      trimmed = value
      sub(/[ \t]+$/, "", trimmed)
      sub(/\\$/, "", trimmed)
      return trimmed
    }

    function tokenize_env(value, result, position, character, quote, escaped, token, count, started, next_character) {
      quote = ""
      escaped = 0
      token = ""
      count = 0
      started = 0

      for (position = 1; position <= length(value); position++) {
        character = substr(value, position, 1)
        if (escaped) {
          token = token character
          escaped = 0
          started = 1
          continue
        }
        if (quote == "") {
          if (character == "\\") {
            escaped = 1
            started = 1
          } else if (character == "\"" || character == "\047") {
            quote = character
            started = 1
          } else if (character ~ /[ \t]/) {
            if (started) {
              result[++count] = token
              token = ""
              started = 0
            }
          } else {
            token = token character
            started = 1
          }
        } else if (character == quote) {
          quote = ""
        } else if (quote == "\"" && character == "\\") {
          next_character = substr(value, position + 1, 1)
          if (next_character == "\"" || next_character == "$" || next_character == "\\") {
            escaped = 1
          } else {
            token = token character
          }
        } else {
          token = token character
        }
      }

      if (escaped || quote != "") {
        return -1
      }
      if (started) {
        result[++count] = token
      }
      return count
    }

    function process_instruction(value, line, instruction, body, token_count, modern, token_index, separator, key, assigned_value) {
      line = value
      sub(/^[ \t]+/, "", line)
      if (!match(line, /^[^ \t]+/)) {
        return
      }

      instruction = toupper(substr(line, 1, RLENGTH))
      if (instruction != "ENV") {
        return
      }

      body = substr(line, RLENGTH + 1)
      sub(/^[ \t]+/, "", body)
      token_count = tokenize_env(body, tokens)
      if (token_count < 1) {
        parse_error("не удалось однозначно разобрать ENV-инструкцию final runtime stage")
      }

      modern = index(tokens[1], "=") > 0
      if (modern) {
        for (token_index = 1; token_index <= token_count; token_index++) {
          separator = index(tokens[token_index], "=")
          if (separator < 2) {
            parse_error("не удалось однозначно разобрать ENV key=value в final runtime stage")
          }
          key = substr(tokens[token_index], 1, separator - 1)
          assigned_value = substr(tokens[token_index], separator + 1)
          if (key !~ /^[A-Za-z_][A-Za-z0-9_]*$/) {
            parse_error("ENV-инструкция final runtime stage содержит недопустимое имя переменной")
          }
          if (key == "GOTOOLCHAIN") {
            effective_value = assigned_value
            found = 1
          }
        }
        return
      }

      key = tokens[1]
      if (key !~ /^[A-Za-z_][A-Za-z0-9_]*$/ || token_count < 2) {
        parse_error("не удалось однозначно разобрать устаревший ENV-синтаксис final runtime stage")
      }
      assigned_value = tokens[2]
      for (token_index = 3; token_index <= token_count; token_index++) {
        assigned_value = assigned_value " " tokens[token_index]
      }
      if (key == "GOTOOLCHAIN") {
        effective_value = assigned_value
        found = 1
      }
    }

    {
      line = $0
      sub(/\r$/, "", line)
      if (line ~ /^[ \t]*($|#)/) {
        next
      }
      if (has_line_continuation(line)) {
        logical = logical strip_line_continuation(line)
        next
      }
      logical = logical line
      process_instruction(logical)
      logical = ""
    }

    END {
      if (parse_failed) {
        exit 2
      }
      if (logical != "") {
        print "незавершённая логическая Dockerfile-инструкция в final runtime stage" > "/dev/stderr"
        exit 2
      }
      if (!found) {
        print "final runtime stage не задаёт GOTOOLCHAIN" > "/dev/stderr"
        exit 2
      }
      print effective_value
    }
  '
}

require_final_runtime_stage_contract() {
  local path="$1"
  local expected_from="$2"
  local dockerfile="$repo_root/$path"
  local stage_info from_line actual_from expected_from_body runtime_stage
  local runtime_env runtime_copy runtime_check
  local env_count copy_count check_count
  local env_line copy_line check_line effective_gotoolchain

  if ! validate_dockerfile_lexical_boundary "$dockerfile"; then
    fail "$path содержит неподдерживаемый байт Dockerfile"
  fi

  if ! validate_dockerfile_parser_boundary "$dockerfile"; then
    fail "$path выходит за границу доверия встроенного Dockerfile parser"
  fi

  if ! stage_info="$(final_runtime_stage "$dockerfile")"; then
    fail "$path содержит неподдерживаемую или неоднозначную Dockerfile-конструкцию"
  fi
  IFS=$'\t' read -r from_line actual_from <<<"$stage_info"
  expected_from_body="${expected_from#FROM }"
  [[ "$actual_from" == "$expected_from_body" ]] || fail "$path должен завершаться stage '$expected_from', найден 'FROM $actual_from'"

  runtime_stage="$(tail -n +"$from_line" "$dockerfile")"
  runtime_env="ENV GOTOOLCHAIN=local"
  runtime_copy="COPY --from=go-tools /usr/local/go /usr/local/go"
  runtime_check="RUN test \"\$(/usr/local/go/bin/go env GOVERSION)\" = \"go$go_version\" && test \"\$(/usr/local/go/bin/go env GOTOOLCHAIN)\" = \"local\""

  env_count="$(grep -Fxc "$runtime_env" <<<"$runtime_stage" || true)"
  copy_count="$(grep -Fxc "$runtime_copy" <<<"$runtime_stage" || true)"
  check_count="$(grep -Fxc "$runtime_check" <<<"$runtime_stage" || true)"
  [[ "$env_count" == 1 ]] || fail "$path final runtime stage должен содержать точный '$runtime_env' ровно один раз"
  [[ "$copy_count" == 1 ]] || fail "$path final runtime stage должен копировать закреплённый Go toolchain ровно один раз"
  [[ "$check_count" == 1 ]] || fail "$path final runtime stage должен закрыто проверять точные GOVERSION и GOTOOLCHAIN"

  env_line="$(grep -nFx "$runtime_env" <<<"$runtime_stage" | cut -d: -f1)"
  copy_line="$(grep -nFx "$runtime_copy" <<<"$runtime_stage" | cut -d: -f1)"
  check_line="$(grep -nFx "$runtime_check" <<<"$runtime_stage" | cut -d: -f1)"
  ((env_line < copy_line)) || fail "$path должен задавать GOTOOLCHAIN=local до копирования Go в final runtime stage"
  ((copy_line < check_line)) || fail "$path должен проверять скопированный Go toolchain до выпуска final runtime stage"

  if ! effective_gotoolchain="$(effective_final_stage_gotoolchain "$dockerfile" "$from_line")"; then
    fail "$path final runtime stage содержит неоднозначный ENV-синтаксис"
  fi
  [[ "$effective_gotoolchain" == local ]] || fail "$path final runtime stage завершает GOTOOLCHAIN значением '$effective_gotoolchain' вместо 'local'"
}

while (($# > 0)); do
  case "$1" in
    --root)
      (($# >= 2)) || fail "для --root нужен путь"
      repo_root="$(cd "$2" && pwd)"
      shift 2
      ;;
    --static-only)
      static_only=true
      shift
      ;;
    *)
      fail "неизвестный аргумент $1"
      ;;
  esac
done

canonical_files=(
  go.mod
  Makefile
  services/external/bot-service/Dockerfile
  services/jobs/agent-runner/Dockerfile
  deploy/images/agent-runner/Dockerfile
  services/external/bot-service/internal/domain/service/prompt_template.go
  services/external/bot-service/internal/domain/service/slash_test.go
  docs/design-guidelines/common/external_dependencies_catalog.md
)
for path in "${canonical_files[@]}"; do
  require_file "$path"
done

go_version="$(awk '$1 == "go" { print $2 }' "$repo_root/go.mod")"
explicit_toolchain="$(awk '$1 == "toolchain" { print $2 }' "$repo_root/go.mod")"
[[ "$go_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "go.mod должен закреплять полную patch-версию Go"
[[ -z "$explicit_toolchain" ]] || fail "go.mod содержит отдельный toolchain $explicit_toolchain вместо подразумеваемого go$go_version"
version_at_least "$go_version" "$security_floor" || fail "минимальная версия Go $go_version ниже security floor $security_floor"

require_line Makefile "GO_MIN_VERSION := $go_version"
require_line Makefile "GO_TOOLCHAIN := go$go_version"

govulncheck_version="$(awk '$1 == "GOVULNCHECK_VERSION" && $2 == ":=" { print $3 }' "$repo_root/Makefile")"
[[ "$govulncheck_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "Makefile должен закреплять точную версию GOVULNCHECK_VERSION"
catalog_govulncheck_version="$(awk -F'`' '$0 ~ /^\| `govulncheck` \|/ { print $4 }' "$repo_root/docs/design-guidelines/common/external_dependencies_catalog.md")"
[[ "$catalog_govulncheck_version" == "$govulncheck_version" ]] || fail "Makefile и каталог зависимостей закрепляют разные версии govulncheck"
require_line Makefile $'\t$(if $(filter file,$(origin GOVULNCHECK_VERSION)),,$(error GOVULNCHECK_VERSION нельзя переопределять))'
require_line Makefile $'\tenv -u GOFLAGS GOENV=off GOWORK=off go run \'golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)\' -mode=source -scan=symbol -show=traces,version ./...'

go_image="golang:$go_version-alpine"
require_line services/external/bot-service/Dockerfile "ARG GOLANG_IMAGE=$go_image"
require_final_runtime_stage_contract services/jobs/agent-runner/Dockerfile "FROM node:24-bookworm"
require_final_runtime_stage_contract deploy/images/agent-runner/Dockerfile "FROM node:24-alpine"
require_count services/jobs/agent-runner/Dockerfile "FROM $go_image" 2
require_count deploy/images/agent-runner/Dockerfile "FROM $go_image" 1
require_count services/external/bot-service/Dockerfile "ENV GOTOOLCHAIN=local" 2
require_count services/jobs/agent-runner/Dockerfile "ENV GOTOOLCHAIN=local" 3
require_count deploy/images/agent-runner/Dockerfile "ENV GOTOOLCHAIN=local" 2
require_count services/external/bot-service/Dockerfile "RUN test \"\$(go env GOVERSION)\" = \"go$go_version\"" 1
require_count services/jobs/agent-runner/Dockerfile "RUN test \"\$(go env GOVERSION)\" = \"go$go_version\"" 2
require_count deploy/images/agent-runner/Dockerfile "RUN test \"\$(go env GOVERSION)\" = \"go$go_version\"" 1

while IFS= read -r image; do
  [[ "$image" == "$go_image" ]] || fail "обнаружен рассинхронизированный или плавающий Go image: $image"
done < <(
  grep -RhoE 'golang:[0-9]+\.[0-9]+(\.[0-9]+)?-alpine' \
    "$repo_root/services" "$repo_root/deploy" \
    --include=Dockerfile | sort -u
)

prompt_version="$(sed -n 's/.*Name: "Go toolchain".*Version: "\([^"]*\)".*/\1/p' "$repo_root/services/external/bot-service/internal/domain/service/prompt_template.go")"
[[ "$prompt_version" == "$go_version" ]] || fail "runtime prompt закрепляет Go $prompt_version вместо $go_version"
require_count services/external/bot-service/internal/domain/service/slash_test.go "\`go\` $go_version" 1
require_count services/external/bot-service/internal/domain/service/slash_test.go "\"go=$go_version\"" 1

catalog_version="$(awk -F'`' '$0 ~ /^\| `go` \|/ { print $4 }' "$repo_root/docs/design-guidelines/common/external_dependencies_catalog.md")"
[[ "$catalog_version" == "$go_version" ]] || fail "каталог зависимостей закрепляет Go $catalog_version вместо $go_version"
require_count docs/design-guidelines/common/external_dependencies_catalog.md "\`$go_image\`" 2

if [[ "$static_only" == true ]]; then
  echo "PASS: канонические источники Go синхронизированы на $go_version"
  exit 0
fi

selected_toolchain="$(env -u GOFLAGS GOENV=off GOWORK=off go env GOVERSION)" || fail "не удалось определить выбранный Go toolchain"
[[ "$selected_toolchain" =~ ^go([0-9]+\.[0-9]+\.[0-9]+)$ ]] || fail "неподдерживаемый Go toolchain $selected_toolchain"
selected_version="${BASH_REMATCH[1]}"
version_at_least "$selected_version" "$go_version" || fail "выбранный Go toolchain $selected_version ниже обязательной версии $go_version"

echo "PASS: канонические источники Go синхронизированы на $go_version; выбран go$selected_version"

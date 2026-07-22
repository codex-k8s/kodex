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

validate_toolchain_stage_contracts() {
  local dockerfile="$1"
  local expected_source_from="$2"
  local expected_tools_from="$3"
  local final_from_line="$4"

  LC_ALL=C awk \
    -v expected_source_from="$expected_source_from" \
    -v expected_tools_from="$expected_tools_from" \
    -v final_from_line="$final_from_line" '
    function contract_error(message) {
      print message > "/dev/stderr"
      contract_failed = 1
      exit 2
    }

    function has_line_continuation(value, trimmed, position, slash_count) {
      trimmed = value
      sub(/[ \t]+$/, "", trimmed)
      slash_count = 0
      for (position = length(trimmed); position > 0 && substr(trimmed, position, 1) == "\\"; position--) {
        slash_count++
      }
      return slash_count == 1
    }

    function strip_line_continuation(value, trimmed) {
      trimmed = value
      sub(/[ \t]+$/, "", trimmed)
      sub(/\\$/, "", trimmed)
      return trimmed
    }

    function process_instruction(value, start_line, line, instruction, body, token_count, alias) {
      line = value
      sub(/^[ \t]+/, "", line)
      if (!match(line, /^[^ \t]+/)) {
        return
      }

      instruction = toupper(substr(line, 1, RLENGTH))
      body = substr(line, RLENGTH + 1)
      sub(/^[ \t]+/, "", body)
      sub(/[ \t]+$/, "", body)

      if (instruction != "FROM") {
        if (current_stage == "source") {
          contract_error("immutable stage alias go-toolchain-source должна содержать только точную FROM-инструкцию")
        }
        if (current_stage == "tools") {
          if (instruction == "COPY" || instruction == "ADD") {
            contract_error("stage alias go-tools не должна содержать COPY или ADD; инструменты собираются только из закреплённого source")
          }
          if (instruction == "SHELL") {
            contract_error("stage alias go-tools не должна переопределять SHELL")
          }
          if (instruction == "RUN") {
            tools_run_count++
          }
        }
        return
      }

      token_count = split(body, tokens, /[ \t]+/)
      alias = ""
      if (token_count >= 3 && toupper(tokens[token_count - 1]) == "AS") {
        alias = toupper(tokens[token_count])
      }
      current_stage = ""

      # BuildKit v0.29.0 приводит stage name к нижнему регистру и хранит
      # привязку без учёта регистра. FROM flags извлекаются до этих аргументов,
      # поэтому alias всегда остаётся последним токеном после AS.
      if (alias == "GO-TOOLCHAIN-SOURCE") {
        source_alias_count++
        source_alias_line = start_line
        current_stage = "source"
        if ("FROM " body == expected_source_from) {
          canonical_source_count++
        }
      }
      if (alias == "GO-TOOLS") {
        tools_alias_count++
        tools_alias_line = start_line
        current_stage = "tools"
        if ("FROM " body == expected_tools_from) {
          canonical_tools_count++
        }
      }
    }

    {
      line = $0
      sub(/\r$/, "", line)
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
      if (contract_failed) {
        exit 2
      }
      if (logical != "") {
        print "незавершённая логическая Dockerfile-инструкция при проверке toolchain stages" > "/dev/stderr"
        exit 2
      }
      if (source_alias_count != 1) {
        print "Dockerfile должен объявлять immutable stage alias go-toolchain-source ровно один раз; найдено " (source_alias_count + 0) > "/dev/stderr"
        exit 2
      }
      if (canonical_source_count != 1) {
        print "immutable stage alias go-toolchain-source должна иметь точную закреплённую форму \047" expected_source_from "\047" > "/dev/stderr"
        exit 2
      }
      if (tools_alias_count != 1) {
        print "Dockerfile должен объявлять логическую stage alias go-tools ровно один раз; найдено " (tools_alias_count + 0) > "/dev/stderr"
        exit 2
      }
      if (canonical_tools_count != 1) {
        print "stage alias go-tools должна иметь точную привязку \047" expected_tools_from "\047 к immutable source" > "/dev/stderr"
        exit 2
      }
      if (source_alias_line >= tools_alias_line || tools_alias_line >= final_from_line) {
        print "immutable source, go-tools и final runtime stage должны быть объявлены в каноническом порядке" > "/dev/stderr"
        exit 2
      }
      if (tools_run_count != 2) {
        print "stage alias go-tools должна содержать ровно две самостоятельные RUN-инструкции закреплённого tools-контракта; найдено " (tools_run_count + 0) > "/dev/stderr"
        exit 2
      }
    }
  ' "$dockerfile"
}

validate_final_runtime_stage_contract_instructions() {
  local dockerfile="$1"
  local from_line="$2"
  local expected_env="$3"
  local expected_copy="$4"
  local expected_check="$5"
  local required_go_tools_copy="$6"
  local allowed_copy_two="$7"
  local allowed_tail_one="$8"
  local allowed_tail_two="$9"
  local allowed_tail_three="${10}"

  tail -n +"$from_line" "$dockerfile" | LC_ALL=C awk \
    -v expected_env="$expected_env" \
    -v expected_copy="$expected_copy" \
    -v expected_check="$expected_check" \
    -v required_go_tools_copy="$required_go_tools_copy" \
    -v allowed_copy_two="$allowed_copy_two" \
    -v allowed_tail_one="$allowed_tail_one" \
    -v allowed_tail_two="$allowed_tail_two" \
    -v allowed_tail_three="$allowed_tail_three" '
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

    function contract_error(message) {
      print message > "/dev/stderr"
      contract_failed = 1
      exit 2
    }

    function process_instruction(value, line, instruction, body, canonical, token_count, modern, token_index, separator, key, assigned_value, was_after_check) {
      line = value
      sub(/^[ \t]+/, "", line)
      if (!match(line, /^[^ \t]+/)) {
        return
      }

      instruction = toupper(substr(line, 1, RLENGTH))
      body = substr(line, RLENGTH + 1)
      sub(/^[ \t]+/, "", body)
      sub(/[ \t]+$/, "", body)
      canonical = instruction " " body
      instruction_index++
      instruction_types[instruction_index] = instruction
      was_after_check = check_seen

      if (canonical == expected_env) {
        env_count++
        env_index = instruction_index
      }
      if (canonical == expected_copy) {
        copy_count++
        copy_index = instruction_index
      }
      if (canonical == required_go_tools_copy) {
        required_go_tools_copy_count++
      }
      if (canonical == expected_check) {
        check_count++
        check_index = instruction_index
        check_seen = 1
      } else if (was_after_check) {
        tail_count++
        tail_values[tail_count] = canonical
        if (instruction == "RUN" || instruction == "COPY" || instruction == "ADD") {
          post_check_write_instruction = instruction
        }
      }

      if (instruction == "SHELL") {
        shell_count++
      }

      if ((instruction == "COPY" || instruction == "ADD") && \
          !was_after_check && canonical != expected_copy && canonical != required_go_tools_copy && canonical != allowed_copy_two) {
        if (index(body, "/usr/local/go") > 0) {
          contract_error("final runtime stage содержит неразрешённую " instruction "-инструкцию с /usr/local/go")
        }
        contract_error("final runtime stage содержит неразрешённую " instruction "-инструкцию вне точного списка разрешённых источников")
      }

      if (instruction != "ENV") {
        return
      }

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
      if (parse_failed || contract_failed) {
        exit 2
      }
      if (logical != "") {
        print "незавершённая логическая Dockerfile-инструкция в final runtime stage" > "/dev/stderr"
        exit 2
      }
      if (env_count != 1) {
        print "final runtime stage должен содержать точную самостоятельную логическую инструкцию \047" expected_env "\047 ровно один раз" > "/dev/stderr"
        exit 2
      }
      if (copy_count != 1) {
        print "final runtime stage должен копировать закреплённый Go toolchain одной точной самостоятельной логической COPY-инструкцией" > "/dev/stderr"
        exit 2
      }
      if (required_go_tools_copy_count != 1) {
        print "final runtime stage должен копировать закреплённые Go tools одной точной самостоятельной логической COPY-инструкцией из stage alias go-tools" > "/dev/stderr"
        exit 2
      }
      if (check_count != 1) {
        print "final runtime stage должен закрыто проверять точные GOVERSION и GOTOOLCHAIN одной самостоятельной логической RUN-инструкцией" > "/dev/stderr"
        exit 2
      }
      if (env_index >= copy_index) {
        print "final runtime stage должен задавать GOTOOLCHAIN=local до логической COPY-инструкции Go toolchain" > "/dev/stderr"
        exit 2
      }
      if (copy_index >= check_index) {
        print "final runtime stage должен проверять скопированный Go toolchain после логической COPY-инструкции" > "/dev/stderr"
        exit 2
      }
      if (shell_count != 0) {
        print "final runtime stage не должна содержать SHELL: обязательный RUN test использует только canonical shell" > "/dev/stderr"
        exit 2
      }
      if (copy_index + 1 != check_index) {
        for (between_index = copy_index + 1; between_index < check_index; between_index++) {
          if (instruction_types[between_index] == "RUN" || instruction_types[between_index] == "COPY" || instruction_types[between_index] == "ADD") {
            print "final runtime stage содержит write-capable " instruction_types[between_index] " между trusted COPY Go toolchain и exact RUN test" > "/dev/stderr"
            exit 2
          }
        }
        print "exact RUN test должен непосредственно следовать за trusted COPY Go toolchain" > "/dev/stderr"
        exit 2
      }
      if (post_check_write_instruction != "") {
        print "final runtime stage содержит write-capable " post_check_write_instruction " после обязательного RUN test" > "/dev/stderr"
        exit 2
      }
      if (!found) {
        print "final runtime stage не задаёт GOTOOLCHAIN" > "/dev/stderr"
        exit 2
      }
      if (effective_value != "local") {
        print effective_value
        exit 0
      }
      expected_tail_count = 0
      if (allowed_tail_one != "") {
        expected_tail_values[++expected_tail_count] = allowed_tail_one
      }
      if (allowed_tail_two != "") {
        expected_tail_values[++expected_tail_count] = allowed_tail_two
      }
      if (allowed_tail_three != "") {
        expected_tail_values[++expected_tail_count] = allowed_tail_three
      }
      if (tail_count != expected_tail_count) {
        print "после обязательного RUN test разрешён только точный metadata tail текущего Dockerfile" > "/dev/stderr"
        exit 2
      }
      for (tail_index = 1; tail_index <= expected_tail_count; tail_index++) {
        if (tail_values[tail_index] != expected_tail_values[tail_index]) {
          print "после обязательного RUN test разрешён только точный metadata tail текущего Dockerfile" > "/dev/stderr"
          exit 2
        }
      }
      print effective_value
    }
  '
}

require_final_runtime_stage_contract() {
  local path="$1"
  local expected_from="$2"
  local required_go_tools_copy="$3"
  local allowed_copy_two="${4:-}"
  local allowed_tail_one="${5:-}"
  local allowed_tail_two="${6:-}"
  local allowed_tail_three="${7:-}"
  local dockerfile="$repo_root/$path"
  local stage_info from_line actual_from expected_from_body
  local runtime_env runtime_copy runtime_check
  local toolchain_stages_result contract_result effective_gotoolchain

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

  if ! toolchain_stages_result="$(validate_toolchain_stage_contracts \
    "$dockerfile" \
    "FROM $go_image AS go-toolchain-source" \
    "FROM go-toolchain-source AS go-tools" \
    "$from_line" 2>&1)"; then
    printf '%s: %s\n' "$path" "$toolchain_stages_result" >&2
    fail "$path не связывает immutable source, go-tools и final runtime stage однозначным контрактом"
  fi

  expected_from_body="${expected_from#FROM }"
  [[ "$actual_from" == "$expected_from_body" ]] || fail "$path должен завершаться stage '$expected_from', найден 'FROM $actual_from'"

  runtime_env="ENV GOTOOLCHAIN=local"
  runtime_copy="COPY --from=go-toolchain-source /usr/local/go /usr/local/go"
  runtime_check="RUN test \"\$(/usr/local/go/bin/go env GOVERSION)\" = \"go$go_version\" && test \"\$(/usr/local/go/bin/go env GOTOOLCHAIN)\" = \"local\""

  if ! contract_result="$(validate_final_runtime_stage_contract_instructions \
    "$dockerfile" \
    "$from_line" \
    "$runtime_env" \
    "$runtime_copy" \
    "$runtime_check" \
    "$required_go_tools_copy" \
    "$allowed_copy_two" \
    "$allowed_tail_one" \
    "$allowed_tail_two" \
    "$allowed_tail_three" 2>&1)"; then
    if grep -Eq 'ENV-инструкц|разобрать ENV' <<<"$contract_result"; then
      fail "$path final runtime stage содержит неоднозначный ENV-синтаксис"
    fi
    printf '%s: %s\n' "$path" "$contract_result" >&2
    fail "$path final runtime stage не соответствует контракту логических Dockerfile-инструкций"
  fi
  effective_gotoolchain="$contract_result"
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
require_final_runtime_stage_contract \
  services/jobs/agent-runner/Dockerfile \
  "FROM node:24-bookworm" \
  "COPY --from=go-tools /tool-bin/ /usr/local/bin/" \
  "COPY --from=builder /out/matter-codex-agent-runner /usr/local/bin/matter-codex-agent-runner" \
  'USER ${MATTERCODEX_AGENT_RUNNER_UID}:${MATTERCODEX_AGENT_RUNNER_GID}' \
  'ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/matter-codex-agent-runner"]'
require_final_runtime_stage_contract \
  deploy/images/agent-runner/Dockerfile \
  "FROM node:24-alpine" \
  "COPY --from=go-tools /tool-bin/ /usr/local/bin/" \
  "" \
  'USER ${MATTERCODEX_AGENT_RUNNER_UID}:${MATTERCODEX_AGENT_RUNNER_GID}' \
  'CMD ["sh"]'
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

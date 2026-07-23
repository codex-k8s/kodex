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

require_source_commitment() {
  local path="$1"
  local expected_sha256="$2"
  local actual_sha256

  actual_sha256="$(sha256sum "$repo_root/$path" | awk '{print $1}')"
  [[ "$actual_sha256" == "$expected_sha256" ]] || \
    fail "$path изменён без обновления полного source commitment защищённого agent-runner build flow"
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
  local expected_go_image="$2"
  local expected_work_run_count="$3"
  local expected_work_copy_count="$4"
  local expected_protected_build_run="$5"

  LC_ALL=C awk \
    -v expected_go_image="$expected_go_image" \
    -v expected_work_run_count="$expected_work_run_count" \
    -v expected_work_copy_count="$expected_work_copy_count" \
    -v expected_protected_build_run="$expected_protected_build_run" '
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

    BEGIN {
      stage_index = -1
    }

    function process_instruction(value, start_line, line, instruction, body, canonical, normalized, token_count, alias) {
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
      normalized = canonical
      gsub(/[ \t]+/, " ", normalized)

      if (instruction != "FROM") {
        if (stage_index == 0) {
          protected_instruction_count++
          protected_last_instruction = normalized
          if (normalized == expected_protected_build_run) {
            protected_build_count++
          }
        }
        if (instruction == "RUN" && toupper(body) ~ /^--MOUNT([= \t]|$)/) {
          contract_error("Dockerfile не должен содержать BuildKit-only RUN mount: штатная сборка выполняется Kaniko")
        }
        if (stage_index == 1) {
          work_contract = work_contract (work_contract == "" ? "" : "\n") line
          if (instruction == "COPY") {
            work_copy_count++
            if (toupper(body) ~ /^--FROM([= \t]|$)/) {
              contract_error("work stage не должна получать данные через COPY --from")
            }
          }
          if (instruction == "ADD") {
            contract_error("work stage не должна содержать ADD")
          }
          if (instruction == "SHELL") {
            contract_error("work stage не должна переопределять SHELL")
          }
          if (instruction == "RUN") {
            work_run_count++
          }
        }
        return
      }

      stage_index++
      token_count = split(body, tokens, /[ \t]+/)
      alias = ""
      if (token_count >= 3 && toupper(tokens[token_count - 1]) == "AS") {
        alias = tokens[token_count]
      }
      if (stage_index == 0 && body == expected_go_image " AS scratch") {
        canonical_source_count++
      }
      if (stage_index == 1 && body == expected_go_image " AS context") {
        canonical_work_count++
      }
      if ((stage_index == 0 && alias != "scratch") || (stage_index == 1 && alias != "context") || (stage_index > 1 && alias != "")) {
        contract_error("Dockerfile stages должны использовать только защищённые BuildKit aliases scratch/context на stages 0/1")
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
      if (canonical_source_count != 1) {
        print "protected builder stage 0 должна иметь точную защищённую форму \047FROM " expected_go_image " AS scratch\047" > "/dev/stderr"
        exit 2
      }
      if (protected_instruction_count != 1 || protected_build_count != 1 || protected_last_instruction != expected_protected_build_run) {
        print "protected builder stage 0 должна первой и единственной инструкцией собирать trusted Go toolchain guard из exact source закреплённым compiler argv/environment" > "/dev/stderr"
        exit 2
      }
      if (stage_index != 2) {
        print "Dockerfile должен содержать ровно три stages; последняя ожидаемая stage имеет индекс 2, найден " stage_index > "/dev/stderr"
        exit 2
      }
      if (canonical_work_count != 1) {
        print "work stage 1 должна иметь точную защищённую форму \047FROM " expected_go_image " AS context\047" > "/dev/stderr"
        exit 2
      }
      if (work_run_count != expected_work_run_count) {
        print "work stage должна содержать точное число RUN-инструкций; найдено " (work_run_count + 0) ", ожидается " expected_work_run_count > "/dev/stderr"
        exit 2
      }
      if (work_copy_count != expected_work_copy_count) {
        print "work stage должна содержать точное число локальных COPY-инструкций; найдено " (work_copy_count + 0) ", ожидается " expected_work_copy_count > "/dev/stderr"
        exit 2
      }
      printf "%s", work_contract
    }
  ' "$dockerfile"
}

validate_final_runtime_stage_contract_instructions() {
  local dockerfile="$1"
  local from_line="$2"
  local expected_bootstrap_env="$3"
  local expected_final_env="$4"
  local expected_workdir="$5"
  local expected_bootstrap_copy="$6"
  local expected_guard_copy="$7"
  local expected_clean="$8"
  local expected_trusted_copy="$9"
  local expected_verify="${10}"
  local required_go_tools_copy="${11}"
  local allowed_copy_two="${12}"
  local expected_run_count="${13}"
  local expected_copy_count="${14}"
  local allowed_tail_one="${15}"
  local allowed_tail_two="${16}"
  local allowed_tail_three="${17}"

  tail -n +"$from_line" "$dockerfile" | LC_ALL=C awk \
    -v expected_bootstrap_env="$expected_bootstrap_env" \
    -v expected_final_env="$expected_final_env" \
    -v expected_workdir="$expected_workdir" \
    -v expected_bootstrap_copy="$expected_bootstrap_copy" \
    -v expected_guard_copy="$expected_guard_copy" \
    -v expected_clean="$expected_clean" \
    -v expected_trusted_copy="$expected_trusted_copy" \
    -v expected_verify="$expected_verify" \
    -v required_go_tools_copy="$required_go_tools_copy" \
    -v allowed_copy_two="$allowed_copy_two" \
    -v expected_run_count="$expected_run_count" \
    -v expected_copy_count="$expected_copy_count" \
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

    function process_instruction(value, line, instruction, body, canonical, token_count, modern, token_index, separator, key, assigned_value, was_after_check, is_clean, is_verify) {
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
      gsub(/[ \t]+/, " ", canonical)
      instruction_index++
      instruction_types[instruction_index] = instruction
      was_after_check = verification_complete
      is_clean = canonical == expected_clean
      is_verify = canonical == expected_verify

      if (instruction == "RUN") {
        final_run_count++
      } else if (instruction == "COPY") {
        final_copy_count++
      } else if (instruction == "ADD") {
        final_add_count++
      }

      if (canonical == expected_bootstrap_env) {
        bootstrap_env_count++
        bootstrap_env_index = instruction_index
      }
      if (canonical == expected_final_env) {
        final_env_count++
        final_env_index = instruction_index
      }
      if (canonical == expected_bootstrap_copy) {
        bootstrap_copy_count++
        bootstrap_copy_index = instruction_index
        bootstrap_seen = 1
      }
      if (canonical == expected_guard_copy) {
        guard_copy_count++
        guard_copy_index = instruction_index
        guard_copy_seen = 1
      }
      if (is_clean) {
        clean_count++
        clean_index = instruction_index
      }
      if (canonical == expected_trusted_copy) {
        trusted_copy_count++
        trusted_copy_index = instruction_index
      }
      if (canonical == required_go_tools_copy) {
        required_go_tools_copy_count++
        bootstrap_tools_copy_index = instruction_index
      }
      if (canonical == allowed_copy_two) {
        final_tools_copy_count++
        final_tools_copy_index = instruction_index
        final_tools_copy_seen = 1
      }
      if (is_verify) {
        verify_count++
        verify_index = instruction_index
      }

      if (was_after_check) {
        tail_count++
        tail_values[tail_count] = canonical
        if (instruction == "RUN" || instruction == "COPY" || instruction == "ADD") {
          post_check_write_instruction = instruction
        }
      }

      if (is_verify) {
        verification_complete = 1
      }

      if (instruction == "SHELL") {
        shell_count++
      }

      if (bootstrap_seen && !final_tools_copy_seen && instruction == "RUN") {
        installer_run_between_copies = 1
      }

      if (instruction == "WORKDIR") {
        workdir_count++
        if (canonical == expected_workdir) {
          expected_workdir_count++
        }
      }

      if (instruction == "ARG" && index(body, "MATTERCODEX_AGENT_RUNNER_UID") > 0) {
        if (canonical != "ARG MATTERCODEX_AGENT_RUNNER_UID=10001") {
          contract_error("MATTERCODEX_AGENT_RUNNER_UID должен иметь единственное точное значение 10001")
        }
        runner_uid_arg_count++
      }
      if (instruction == "ARG" && index(body, "MATTERCODEX_AGENT_RUNNER_GID") > 0) {
        if (canonical != "ARG MATTERCODEX_AGENT_RUNNER_GID=10001") {
          contract_error("MATTERCODEX_AGENT_RUNNER_GID должен иметь единственное точное значение 10001")
        }
        runner_gid_arg_count++
      }
      if (instruction == "ENV" && (index(body, "MATTERCODEX_AGENT_RUNNER_UID") > 0 || index(body, "MATTERCODEX_AGENT_RUNNER_GID") > 0)) {
        contract_error("effective runtime UID/GID нельзя переопределять через ENV")
      }
      if (instruction == "USER") {
        runtime_user_count++
        if (canonical == "USER 10001:10001") {
          literal_runtime_user_count++
          runtime_user_index = instruction_index
        }
      }

      if ((instruction == "COPY" || instruction == "ADD") && \
          !was_after_check && canonical != expected_bootstrap_copy && canonical != expected_guard_copy && canonical != expected_trusted_copy && canonical != required_go_tools_copy && canonical != allowed_copy_two) {
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
            gotoolchain_count++
          } else if (key == "GOROOT") {
            effective_goroot = assigned_value
            goroot_count++
          } else if (key == "GOENV") {
            effective_goenv = assigned_value
            goenv_count++
          } else if (key == "GOFLAGS") {
            effective_goflags = assigned_value
            goflags_count++
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
        gotoolchain_count++
      } else if (key == "GOROOT") {
        effective_goroot = assigned_value
        goroot_count++
      } else if (key == "GOENV") {
        effective_goenv = assigned_value
        goenv_count++
      } else if (key == "GOFLAGS") {
        effective_goflags = assigned_value
        goflags_count++
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
      if (bootstrap_env_count != 1) {
        print "final runtime stage должен содержать точную самостоятельную bootstrap ENV-инструкцию \047" expected_bootstrap_env "\047 ровно один раз" > "/dev/stderr"
        exit 2
      }
      if (final_env_count != 1) {
        print "final runtime stage должен содержать точную самостоятельную final ENV-инструкцию \047" expected_final_env "\047 ровно один раз" > "/dev/stderr"
        exit 2
      }
      if (bootstrap_copy_count != 1) {
        print "final runtime stage должен содержать ровно один Kaniko-compatible bootstrap COPY Go toolchain" > "/dev/stderr"
        exit 2
      }
      if (guard_copy_count != 1) {
        print "final runtime stage должен содержать ровно один trusted COPY Go toolchain guard" > "/dev/stderr"
        exit 2
      }
      if (clean_count != 1) {
        print "final runtime stage должен выполнять точное trusted очищение GOROOT ровно один раз" > "/dev/stderr"
        exit 2
      }
      if (trusted_copy_count != 1) {
        print "final runtime stage должен содержать ровно один final trusted COPY Go toolchain" > "/dev/stderr"
        exit 2
      }
      if (required_go_tools_copy_count != 1 || final_tools_copy_count != 1) {
        print "final runtime stage должен копировать единый committed набор runner/tools сначала для installer, затем в trusted staging" > "/dev/stderr"
        exit 2
      }
      if (verify_count != 1) {
        print "final runtime stage должен выполнять точную shell-independent проверку GOVERSION/GOTOOLCHAIN/GOROOT и compiler probe ровно один раз" > "/dev/stderr"
        exit 2
      }
      if (post_check_write_instruction != "") {
        print "final runtime stage содержит write-capable " post_check_write_instruction " после обязательной stdout verification" > "/dev/stderr"
        exit 2
      }
      if (final_run_count != expected_run_count) {
        print "final runtime stage содержит " (final_run_count + 0) " RUN-инструкций; ожидается ровно " expected_run_count > "/dev/stderr"
        exit 2
      }
      if (final_copy_count != expected_copy_count) {
        print "final runtime stage содержит " (final_copy_count + 0) " COPY-инструкций; ожидается ровно " expected_copy_count > "/dev/stderr"
        exit 2
      }
      if (final_add_count != 0) {
        print "final runtime stage не должна содержать ADD-инструкции" > "/dev/stderr"
        exit 2
      }
      if (bootstrap_env_index >= bootstrap_copy_index) {
        print "final runtime stage должен задавать GOTOOLCHAIN=local до bootstrap COPY Go toolchain" > "/dev/stderr"
        exit 2
      }
      if (bootstrap_tools_copy_index >= bootstrap_copy_index) {
        print "первичный COPY Go tools должен предшествовать Kaniko-compatible bootstrap COPY" > "/dev/stderr"
        exit 2
      }
      if (bootstrap_copy_index >= guard_copy_index) {
        print "Kaniko-compatible bootstrap COPY должен предшествовать trusted COPY Go toolchain guard" > "/dev/stderr"
        exit 2
      }
      if (!installer_run_between_copies) {
        print "между bootstrap COPY и final trusted COPY должен выполняться установочный RUN" > "/dev/stderr"
        exit 2
      }
      if (shell_count != 0) {
        print "final runtime stage не должна содержать SHELL: trusted clean/verify закреплены точной exec-form" > "/dev/stderr"
        exit 2
      }
      if (guard_copy_index + 1 != clean_index || clean_index + 1 != trusted_copy_index || trusted_copy_index + 1 != final_tools_copy_index || final_tools_copy_index + 1 != final_env_index || final_env_index + 1 != verify_index) {
        print "trusted guard COPY, topology prepare, final Go COPY, protected staging COPY, final ENV и install/verification должны идти непосредственно друг за другом" > "/dev/stderr"
        exit 2
      }
      if (expected_workdir == "") {
        if (workdir_count != 0) {
          print "final runtime stage не должна содержать WORKDIR" > "/dev/stderr"
          exit 2
        }
      } else if (workdir_count != 1 || expected_workdir_count != 1) {
        print "final runtime stage должна содержать ровно одну точную инструкцию \047" expected_workdir "\047" > "/dev/stderr"
        exit 2
      }
      if (gotoolchain_count != 2 || effective_value != "local") {
        print "final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" > "/dev/stderr"
        exit 2
      }
      if (goroot_count != 1 || effective_goroot != "/usr/local/go") {
        print "final runtime stage должен задавать GOROOT=/usr/local/go ровно один раз в final ENV" > "/dev/stderr"
        exit 2
      }
      if (goenv_count != 1 || effective_goenv != "off") {
        print "final runtime stage должен задавать GOENV=off ровно один раз в final ENV" > "/dev/stderr"
        exit 2
      }
      if (goflags_count != 1 || effective_goflags != "") {
        print "final runtime stage должен очищать GOFLAGS ровно один раз в final ENV" > "/dev/stderr"
        exit 2
      }
      if (runner_uid_arg_count != 1 || runner_gid_arg_count != 1) {
        print "final runtime stage должен содержать единственные точные ARG UID/GID со значением 10001" > "/dev/stderr"
        exit 2
      }
      if (runtime_user_count != 1 || literal_runtime_user_count != 1 || runtime_user_index != verify_index + 1) {
        print "final runtime stage должен закреплять effective USER точной literal инструкцией USER 10001:10001 сразу после trusted install/verification" > "/dev/stderr"
        exit 2
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
        print "после обязательной stdout verification разрешён только точный metadata tail текущего Dockerfile" > "/dev/stderr"
        exit 2
      }
      for (tail_index = 1; tail_index <= expected_tail_count; tail_index++) {
        if (tail_values[tail_index] != expected_tail_values[tail_index]) {
          print "после обязательной stdout verification разрешён только точный metadata tail текущего Dockerfile" > "/dev/stderr"
          exit 2
        }
      }
      print effective_value "\t" effective_goroot "\t" effective_goenv "\t" effective_goflags
    }
  '
}

require_final_runtime_stage_contract() {
  local path="$1"
  local expected_from="$2"
  local expected_work_run_count="$3"
  local expected_work_copy_count="$4"
  local required_go_tools_copy="$5"
  local allowed_copy_two="${6:-}"
  local expected_final_run_count="$7"
  local expected_final_copy_count="$8"
  local expected_workdir="$9"
  local final_runtime_env="${10}"
  local allowed_tail_one="${11:-}"
  local allowed_tail_two="${12:-}"
  local expected_work_contract_sha256="${13}"
  local dockerfile="$repo_root/$path"
  local stage_info from_line actual_from expected_from_body
  local runtime_bootstrap_env runtime_bootstrap_copy runtime_guard_copy runtime_clean
  local runtime_trusted_copy runtime_verify
  local protected_profile
  local toolchain_stages_result actual_work_contract_sha256 contract_result

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
    "$go_image" \
    "$expected_work_run_count" \
    "$expected_work_copy_count" \
    "$trusted_guard_build_run" 2>&1)"; then
    printf '%s: %s\n' "$path" "$toolchain_stages_result" >&2
    fail "$path не связывает защищённые stages scratch/context и final runtime stage однозначным контрактом"
  fi
  actual_work_contract_sha256="$(printf '%s' "$toolchain_stages_result" | sha256sum | awk '{print $1}')"
  [[ "$actual_work_contract_sha256" == "$expected_work_contract_sha256" ]] || \
    fail "$path work stage source-level contract изменён; install/build instructions и tool outputs не имеют закреплённого provenance"

  expected_from_body="${expected_from#FROM }"
  [[ "$actual_from" == "$expected_from_body" ]] || fail "$path должен завершаться stage '$expected_from', найден 'FROM $actual_from'"

  runtime_bootstrap_env="ENV GOTOOLCHAIN=local"
  runtime_bootstrap_copy="COPY --from=0 /usr/local/go/ /opt/mattercodex/bootstrap-go/"
  if [[ "$path" == services/jobs/agent-runner/Dockerfile ]]; then
    protected_profile=services
  else
    protected_profile=deploy
  fi
  runtime_guard_copy="COPY --from=0 /out/mattercodex-go-toolchain-guard /mattercodex-go-toolchain-guard"
  runtime_clean="RUN [\"/mattercodex-go-toolchain-guard\", \"prepare\", \"$protected_profile\"]"
  runtime_trusted_copy="COPY --from=0 /usr/local/go/ /usr/local/go/"
  runtime_verify="RUN [\"/mattercodex-go-toolchain-guard\", \"install\", \"$protected_profile\", \"/usr/local/go/bin/go\"]"

  if ! contract_result="$(validate_final_runtime_stage_contract_instructions \
    "$dockerfile" \
    "$from_line" \
    "$runtime_bootstrap_env" \
    "$final_runtime_env" \
    "$expected_workdir" \
    "$runtime_bootstrap_copy" \
    "$runtime_guard_copy" \
    "$runtime_clean" \
    "$runtime_trusted_copy" \
    "$runtime_verify" \
    "$required_go_tools_copy" \
    "$allowed_copy_two" \
    "$expected_final_run_count" \
    "$expected_final_copy_count" \
    "$allowed_tail_one" \
    "$allowed_tail_two" \
    "" 2>&1)"; then
    if grep -Eq 'не удалось однозначно разобрать (ENV|устаревший ENV)|ENV-инструкция final runtime stage содержит недопустимое имя переменной' <<<"$contract_result"; then
      fail "$path final runtime stage содержит неоднозначный ENV-синтаксис"
    fi
    printf '%s: %s\n' "$path" "$contract_result" >&2
    fail "$path final runtime stage не соответствует контракту логических Dockerfile-инструкций"
  fi
  [[ "$contract_result" == $'local\t/usr/local/go\toff\t' ]] || \
    fail "$path final runtime environment не закрепляет GOTOOLCHAIN=local, GOROOT=/usr/local/go, GOENV=off и пустой GOFLAGS"
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
  scripts/build-agent-runner-image.sh
  scripts/k8s/install-bot-service.sh
  scripts/remote/install-bot-service.sh
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
[[ -x "$repo_root/scripts/build-agent-runner-image.sh" ]] || fail "scripts/build-agent-runner-image.sh должен быть исполняемым"
require_source_commitment scripts/build-agent-runner-image.sh 4523f3b7c484d6b7caea5c64827e358a39452d698f602663a9a75c17be7251a6
require_source_commitment scripts/k8s/install-bot-service.sh 559cf6bcd710cb6b4aa0a1ebec80c9fa988c22f101bb59adab12630f1a15cd34
require_source_commitment scripts/remote/install-bot-service.sh c947d5f1e20090670de6a520b72e9ae9fe10a7bcc0d261f2934a48439258a94e

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
trusted_guard_source_base64="cGFja2FnZSBtYWluCgppbXBvcnQgKAoJImJ5dGVzIgoJImNyeXB0by9zaGEyNTYiCgkiZm10IgoJImlvIgoJIm9zIgoJIm9zL2V4ZWMiCgkicGF0aC9maWxlcGF0aCIKCSJzb3J0IgoJInN0cmluZ3MiCgkic3lzY2FsbCIKKQoKY29uc3QgKAoJdHJ1c3RlZEdPUk9PVCAgICAgID0gIi91c3IvbG9jYWwvZ28iCglib290c3RyYXBHT1JPT1QgICAgPSAiL29wdC9tYXR0ZXJjb2RleC9ib290c3RyYXAtZ28iCglwcm90ZWN0ZWRTdGFnaW5nICAgPSAiL29wdC9tYXR0ZXJjb2RleC9wcm90ZWN0ZWQtYXJ0aWZhY3RzIgoJcHJvdGVjdGVkVGFyZ2V0RGlyID0gIi91c3IvbG9jYWwvYmluIgoJcHJvdGVjdGVkUm9vdCAgICAgID0gIi8iCgl0cnVzdGVkVUlEICAgICAgICAgPSAwCgl0cnVzdGVkR0lEICAgICAgICAgPSAwCikKCnZhciBwcm90ZWN0ZWRUb29scyA9IFtdc3RyaW5newoJImJ1ZiIsCgkiZ29mdW1wdCIsCgkiZ29pbXBvcnRzIiwKCSJnb2xhbmdjaS1saW50IiwKCSJnb29zZSIsCgkiZ3JwY3VybCIsCgkibW9ja2dlbiIsCgkib2FwaS1jb2RlZ2VuIiwKCSJwcm90b2MtZ2VuLWdvIiwKCSJwcm90b2MtZ2VuLWdvLWdycGMiLAoJInNxbGMiLAoJInN0YXRpY2NoZWNrIiwKCSJ5cSIsCn0KCmZ1bmMgZmFpbChmb3JtYXQgc3RyaW5nLCB2YWx1ZXMgLi4uYW55KSB7CglmbXQuRnByaW50Zihvcy5TdGRlcnIsIGZvcm1hdCsiXG4iLCB2YWx1ZXMuLi4pCglvcy5FeGl0KDEpCn0KCmZ1bmMgdHJ1c3RlZEdvQ29tbWFuZChnb0V4ZWN1dGFibGUgc3RyaW5nLCBhcmd1bWVudHMgLi4uc3RyaW5nKSAqZXhlYy5DbWQgewoJY29tbWFuZCA6PSBleGVjLkNvbW1hbmQoZ29FeGVjdXRhYmxlLCBhcmd1bWVudHMuLi4pCgljb21tYW5kLkVudiA9IFtdc3RyaW5newoJCSJQQVRIPS91c3IvbG9jYWwvZ28vYmluOi91c3IvYmluOi9iaW4iLAoJCSJIT01FPS90bXAiLAoJCSJHT1JPT1Q9IiArIHRydXN0ZWRHT1JPT1QsCgkJIkdPRU5WPW9mZiIsCgkJIkdPRkxBR1M9IiwKCQkiR09UT09MQ0hBSU49bG9jYWwiLAoJCSJHT1dPUks9b2ZmIiwKCX0KCXJldHVybiBjb21tYW5kCn0KCmZ1bmMgZXhwZWN0T3V0cHV0KGdvRXhlY3V0YWJsZSwgbGFiZWwsIGV4cGVjdGVkIHN0cmluZywgYXJndW1lbnRzIC4uLnN0cmluZykgewoJb3V0cHV0LCBlcnIgOj0gdHJ1c3RlZEdvQ29tbWFuZChnb0V4ZWN1dGFibGUsIGFyZ3VtZW50cy4uLikuT3V0cHV0KCkKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoIiVzIGZhaWxlZDogJXYiLCBsYWJlbCwgZXJyKQoJfQoJaWYgIWJ5dGVzLkVxdWFsKG91dHB1dCwgW11ieXRlKGV4cGVjdGVkKSkgewoJCWZhaWwoIiVzIG1pc21hdGNoOiBnb3QgJXEsIHdhbnQgJXEiLCBsYWJlbCwgb3V0cHV0LCBleHBlY3RlZCkKCX0KfQoKZnVuYyB2ZXJpZnlHbyhnb0V4ZWN1dGFibGUgc3RyaW5nKSB7CglleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCAiR09WRVJTSU9OIiwgImdvMS4yNi41XG4iLCAiZW52IiwgIkdPVkVSU0lPTiIpCglleHBlY3RPdXRwdXQoZ29FeGVjdXRhYmxlLCAiR09UT09MQ0hBSU4iLCAibG9jYWxcbiIsICJlbnYiLCAiR09UT09MQ0hBSU4iKQoJZXhwZWN0T3V0cHV0KGdvRXhlY3V0YWJsZSwgIkdPUk9PVCIsIHRydXN0ZWRHT1JPT1QrIlxuIiwgImVudiIsICJHT1JPT1QiKQoJZXhwZWN0T3V0cHV0KGdvRXhlY3V0YWJsZSwgImdvIHRvb2wgY29tcGlsZSIsICJjb21waWxlIHZlcnNpb24gZ28xLjI2LjVcbiIsICJ0b29sIiwgImNvbXBpbGUiLCAiLVY9ZnVsbCIpCn0KCmZ1bmMgcHJvdGVjdGVkTmFtZXMocHJvZmlsZSBzdHJpbmcpIFtdc3RyaW5nIHsKCW5hbWVzIDo9IGFwcGVuZChbXXN0cmluZyhuaWwpLCBwcm90ZWN0ZWRUb29scy4uLikKCXN3aXRjaCBwcm9maWxlIHsKCWNhc2UgInNlcnZpY2VzIjoKCQluYW1lcyA9IGFwcGVuZChuYW1lcywgIm1hdHRlci1jb2RleC1hZ2VudC1ydW5uZXIiKQoJY2FzZSAiZGVwbG95IjoKCWRlZmF1bHQ6CgkJZmFpbCgidW5zdXBwb3J0ZWQgcHJvZmlsZSAlcSIsIHByb2ZpbGUpCgl9Cglzb3J0LlN0cmluZ3MobmFtZXMpCglyZXR1cm4gbmFtZXMKfQoKZnVuYyByZXF1aXJlVHJ1c3RlZElkZW50aXR5KCkgewoJaWYgb3MuR2V0ZXVpZCgpICE9IHRydXN0ZWRVSUQgfHwgb3MuR2V0ZWdpZCgpICE9IHRydXN0ZWRHSUQgewoJCWZhaWwoInRydXN0ZWQgZ3VhcmQgcmVxdWlyZXMgdWlkOmdpZCAlZDolZCIsIHRydXN0ZWRVSUQsIHRydXN0ZWRHSUQpCgl9Cn0KCmZ1bmMgc3RhdChwYXRoIHN0cmluZykgKG9zLkZpbGVJbmZvLCAqc3lzY2FsbC5TdGF0X3QpIHsKCWluZm8sIGVyciA6PSBvcy5Mc3RhdChwYXRoKQoJaWYgZXJyICE9IG5pbCB7CgkJZmFpbCgibHN0YXQgJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJc3RhdHVzLCBvayA6PSBpbmZvLlN5cygpLigqc3lzY2FsbC5TdGF0X3QpCglpZiAhb2sgewoJCWZhaWwoInN0YXQgJXM6IHVuc3VwcG9ydGVkIHBsYXRmb3JtIiwgcGF0aCkKCX0KCXJldHVybiBpbmZvLCBzdGF0dXMKfQoKZnVuYyBlbnN1cmVEaXJlY3RvcnkocGF0aCBzdHJpbmcpIHsKCXBhdGggPSBmaWxlcGF0aC5DbGVhbihwYXRoKQoJcm9vdCA6PSBmaWxlcGF0aC5DbGVhbihwcm90ZWN0ZWRSb290KQoJaWYgcGF0aCA9PSByb290IHsKCQl2ZXJpZnlEaXJlY3RvcnkocGF0aCkKCQlyZXR1cm4KCX0KCWlmIHJvb3QgIT0gIi8iICYmICFzdHJpbmdzLkhhc1ByZWZpeChwYXRoLCByb290K3N0cmluZyhvcy5QYXRoU2VwYXJhdG9yKSkgewoJCWZhaWwoInByb3RlY3RlZCBwYXRoICVzIGVzY2FwZXMgcm9vdCAlcyIsIHBhdGgsIHJvb3QpCgl9CglwYXJlbnQgOj0gZmlsZXBhdGguRGlyKHBhdGgpCgllbnN1cmVEaXJlY3RvcnkocGFyZW50KQoJaW5mbywgZXJyIDo9IG9zLkxzdGF0KHBhdGgpCglpZiBlcnIgPT0gbmlsICYmICFpbmZvLklzRGlyKCkgewoJCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwocGF0aCk7IGVyciAhPSBuaWwgewoJCQlmYWlsKCJyZW1vdmUgdW5zYWZlIHBhdGggJXM6ICV2IiwgcGF0aCwgZXJyKQoJCX0KCQlpbmZvID0gbmlsCgl9IGVsc2UgaWYgZXJyICE9IG5pbCAmJiAhb3MuSXNOb3RFeGlzdChlcnIpIHsKCQlmYWlsKCJsc3RhdCAlczogJXYiLCBwYXRoLCBlcnIpCgl9CglpZiBpbmZvID09IG5pbCB7CgkJaWYgZXJyIDo9IG9zLk1rZGlyKHBhdGgsIDBvNzU1KTsgZXJyICE9IG5pbCAmJiAhb3MuSXNFeGlzdChlcnIpIHsKCQkJZmFpbCgibWtkaXIgJXM6ICV2IiwgcGF0aCwgZXJyKQoJCX0KCX0KCWlmIGVyciA6PSBvcy5DaG93bihwYXRoLCB0cnVzdGVkVUlELCB0cnVzdGVkR0lEKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2hvd24gJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJaWYgZXJyIDo9IG9zLkNobW9kKHBhdGgsIDBvNzU1KTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2htb2QgJXM6ICV2IiwgcGF0aCwgZXJyKQoJfQoJdmVyaWZ5RGlyZWN0b3J5KHBhdGgpCn0KCmZ1bmMgdmVyaWZ5RGlyZWN0b3J5KHBhdGggc3RyaW5nKSB7CglpbmZvLCBzdGF0dXMgOj0gc3RhdChwYXRoKQoJaWYgIWluZm8uSXNEaXIoKSB8fCBpbmZvLk1vZGUoKSZvcy5Nb2RlU3ltbGluayAhPSAwIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZGlyZWN0b3J5ICVzIGlzIG5vdCBhIHJlYWwgZGlyZWN0b3J5IiwgcGF0aCkKCX0KCWlmIHN0YXR1cy5VaWQgIT0gdWludDMyKHRydXN0ZWRVSUQpIHx8IHN0YXR1cy5HaWQgIT0gdWludDMyKHRydXN0ZWRHSUQpIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZGlyZWN0b3J5ICVzIGlzIG5vdCByb290LW93bmVkIiwgcGF0aCkKCX0KCWlmIGluZm8uTW9kZSgpLlBlcm0oKSYwbzAyMiAhPSAwIHsKCQlmYWlsKCJwcm90ZWN0ZWQgZGlyZWN0b3J5ICVzIGlzIGdyb3VwL3dvcmxkLXdyaXRhYmxlIiwgcGF0aCkKCX0KfQoKZnVuYyB2ZXJpZnlQYXJlbnRzKHBhdGggc3RyaW5nKSB7Cglyb290IDo9IGZpbGVwYXRoLkNsZWFuKHByb3RlY3RlZFJvb3QpCglmb3IgY3VycmVudCA6PSBmaWxlcGF0aC5DbGVhbihwYXRoKTsgOyBjdXJyZW50ID0gZmlsZXBhdGguRGlyKGN1cnJlbnQpIHsKCQl2ZXJpZnlEaXJlY3RvcnkoY3VycmVudCkKCQlpZiBjdXJyZW50ID09IHJvb3QgewoJCQlyZXR1cm4KCQl9Cgl9Cn0KCmZ1bmMgcmVtb3ZlUHJvdGVjdGVkTGVhdmVzKHByb2ZpbGUgc3RyaW5nKSB7Cglmb3IgXywgbmFtZSA6PSByYW5nZSBwcm90ZWN0ZWROYW1lcyhwcm9maWxlKSB7CgkJcGF0aCA6PSBmaWxlcGF0aC5Kb2luKHByb3RlY3RlZFRhcmdldERpciwgbmFtZSkKCQlpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKHBhdGgpOyBlcnIgIT0gbmlsIHsKCQkJZmFpbCgicmVtb3ZlIHByb3RlY3RlZCBkZXN0aW5hdGlvbiAlczogJXYiLCBwYXRoLCBlcnIpCgkJfQoJfQp9CgpmdW5jIHByZXBhcmUocHJvZmlsZSBzdHJpbmcpIHsKCXJlcXVpcmVUcnVzdGVkSWRlbnRpdHkoKQoJZW5zdXJlRGlyZWN0b3J5KGZpbGVwYXRoLkRpcih0cnVzdGVkR09ST09UKSkKCWVuc3VyZURpcmVjdG9yeShmaWxlcGF0aC5EaXIoYm9vdHN0cmFwR09ST09UKSkKCWVuc3VyZURpcmVjdG9yeShmaWxlcGF0aC5EaXIocHJvdGVjdGVkU3RhZ2luZykpCgllbnN1cmVEaXJlY3RvcnkocHJvdGVjdGVkVGFyZ2V0RGlyKQoJaWYgZXJyIDo9IG9zLlJlbW92ZUFsbCh0cnVzdGVkR09ST09UKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2xlYW4gJXM6ICV2IiwgdHJ1c3RlZEdPUk9PVCwgZXJyKQoJfQoJaWYgZXJyIDo9IG9zLlJlbW92ZUFsbChib290c3RyYXBHT1JPT1QpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJjbGVhbiAlczogJXYiLCBib290c3RyYXBHT1JPT1QsIGVycikKCX0KCWlmIGVyciA6PSBvcy5SZW1vdmVBbGwocHJvdGVjdGVkU3RhZ2luZyk7IGVyciAhPSBuaWwgewoJCWZhaWwoImNsZWFuICVzOiAldiIsIHByb3RlY3RlZFN0YWdpbmcsIGVycikKCX0KCWVuc3VyZURpcmVjdG9yeSh0cnVzdGVkR09ST09UKQoJZW5zdXJlRGlyZWN0b3J5KHByb3RlY3RlZFN0YWdpbmcpCglyZW1vdmVQcm90ZWN0ZWRMZWF2ZXMocHJvZmlsZSkKCXZlcmlmeVBhcmVudHModHJ1c3RlZEdPUk9PVCkKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkU3RhZ2luZykKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkVGFyZ2V0RGlyKQp9CgpmdW5jIHZlcmlmeVJlZ3VsYXIocGF0aCBzdHJpbmcpIChvcy5GaWxlSW5mbywgKnN5c2NhbGwuU3RhdF90KSB7CglpbmZvLCBzdGF0dXMgOj0gc3RhdChwYXRoKQoJaWYgIWluZm8uTW9kZSgpLklzUmVndWxhcigpIHx8IGluZm8uTW9kZSgpJm9zLk1vZGVTeW1saW5rICE9IDAgewoJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCAlcyBpcyBub3QgYSByZWd1bGFyIG5vbi1zeW1saW5rIGZpbGUiLCBwYXRoKQoJfQoJaWYgc3RhdHVzLlVpZCAhPSB1aW50MzIodHJ1c3RlZFVJRCkgfHwgc3RhdHVzLkdpZCAhPSB1aW50MzIodHJ1c3RlZEdJRCkgewoJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCAlcyBpcyBub3Qgcm9vdC1vd25lZCIsIHBhdGgpCgl9CglpZiBzdGF0dXMuTmxpbmsgIT0gMSB7CgkJZmFpbCgicHJvdGVjdGVkIGFydGlmYWN0ICVzIGhhcyAlZCBoYXJkbGlua3MiLCBwYXRoLCBzdGF0dXMuTmxpbmspCgl9CglpZiBpbmZvLk1vZGUoKS5QZXJtKCkmMG8wMjIgIT0gMCB7CgkJZmFpbCgicHJvdGVjdGVkIGFydGlmYWN0ICVzIGlzIGdyb3VwL3dvcmxkLXdyaXRhYmxlIiwgcGF0aCkKCX0KCXJldHVybiBpbmZvLCBzdGF0dXMKfQoKZnVuYyBkaWdlc3QocGF0aCBzdHJpbmcpIFtzaGEyNTYuU2l6ZV1ieXRlIHsKCWZpbGUsIGVyciA6PSBvcy5PcGVuKHBhdGgpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCJvcGVuICVzOiAldiIsIHBhdGgsIGVycikKCX0KCWRlZmVyIGZpbGUuQ2xvc2UoKQoJaGFzaCA6PSBzaGEyNTYuTmV3KCkKCWlmIF8sIGVyciA6PSBpby5Db3B5KGhhc2gsIGZpbGUpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJoYXNoICVzOiAldiIsIHBhdGgsIGVycikKCX0KCXZhciByZXN1bHQgW3NoYTI1Ni5TaXplXWJ5dGUKCWNvcHkocmVzdWx0WzpdLCBoYXNoLlN1bShuaWwpKQoJcmV0dXJuIHJlc3VsdAp9CgpmdW5jIGNvcHlQcm90ZWN0ZWQoc291cmNlLCB0YXJnZXQgc3RyaW5nKSB7Cglzb3VyY2VGaWxlLCBlcnIgOj0gb3MuT3Blbihzb3VyY2UpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCJvcGVuIHByb3RlY3RlZCBzb3VyY2UgJXM6ICV2Iiwgc291cmNlLCBlcnIpCgl9CglkZWZlciBzb3VyY2VGaWxlLkNsb3NlKCkKCXRhcmdldEZpbGUsIGVyciA6PSBvcy5PcGVuRmlsZSh0YXJnZXQsIG9zLk9fV1JPTkxZfG9zLk9fQ1JFQVRFfG9zLk9fRVhDTCwgMG81NTUpCglpZiBlcnIgIT0gbmlsIHsKCQlmYWlsKCJjcmVhdGUgcHJvdGVjdGVkIGRlc3RpbmF0aW9uICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgXywgZXJyIDo9IGlvLkNvcHkodGFyZ2V0RmlsZSwgc291cmNlRmlsZSk7IGVyciAhPSBuaWwgewoJCXRhcmdldEZpbGUuQ2xvc2UoKQoJCWZhaWwoImNvcHkgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgZXJyIDo9IHRhcmdldEZpbGUuU3luYygpOyBlcnIgIT0gbmlsIHsKCQl0YXJnZXRGaWxlLkNsb3NlKCkKCQlmYWlsKCJzeW5jIHByb3RlY3RlZCBhcnRpZmFjdCAlczogJXYiLCB0YXJnZXQsIGVycikKCX0KCWlmIGVyciA6PSB0YXJnZXRGaWxlLkNobW9kKDBvNTU1KTsgZXJyICE9IG5pbCB7CgkJdGFyZ2V0RmlsZS5DbG9zZSgpCgkJZmFpbCgiY2htb2QgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQoJaWYgZXJyIDo9IHRhcmdldEZpbGUuQ2xvc2UoKTsgZXJyICE9IG5pbCB7CgkJZmFpbCgiY2xvc2UgcHJvdGVjdGVkIGFydGlmYWN0ICVzOiAldiIsIHRhcmdldCwgZXJyKQoJfQp9CgpmdW5jIHZlcmlmeUV4YWN0U3RhZ2luZyhuYW1lcyBbXXN0cmluZykgewoJZW50cmllcywgZXJyIDo9IG9zLlJlYWREaXIocHJvdGVjdGVkU3RhZ2luZykKCWlmIGVyciAhPSBuaWwgewoJCWZhaWwoInJlYWQgcHJvdGVjdGVkIHN0YWdpbmc6ICV2IiwgZXJyKQoJfQoJaWYgbGVuKGVudHJpZXMpICE9IGxlbihuYW1lcykgewoJCWZhaWwoInByb3RlY3RlZCBzdGFnaW5nIGhhcyAlZCBhcnRpZmFjdHMsIHdhbnQgJWQiLCBsZW4oZW50cmllcyksIGxlbihuYW1lcykpCgl9Cglmb3IgaW5kZXgsIGVudHJ5IDo9IHJhbmdlIGVudHJpZXMgewoJCWlmIGVudHJ5Lk5hbWUoKSAhPSBuYW1lc1tpbmRleF0gewoJCQlmYWlsKCJwcm90ZWN0ZWQgc3RhZ2luZyBhcnRpZmFjdCAlcSwgd2FudCAlcSIsIGVudHJ5Lk5hbWUoKSwgbmFtZXNbaW5kZXhdKQoJCX0KCX0KfQoKZnVuYyBpbnN0YWxsKHByb2ZpbGUsIGdvRXhlY3V0YWJsZSBzdHJpbmcpIHsKCXJlcXVpcmVUcnVzdGVkSWRlbnRpdHkoKQoJbmFtZXMgOj0gcHJvdGVjdGVkTmFtZXMocHJvZmlsZSkKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkU3RhZ2luZykKCXZlcmlmeVBhcmVudHMocHJvdGVjdGVkVGFyZ2V0RGlyKQoJdmVyaWZ5UGFyZW50cyh0cnVzdGVkR09ST09UKQoJdmVyaWZ5RXhhY3RTdGFnaW5nKG5hbWVzKQoJc291cmNlRGlnZXN0cyA6PSBtYWtlKG1hcFtzdHJpbmddW3NoYTI1Ni5TaXplXWJ5dGUsIGxlbihuYW1lcykpCglmb3IgXywgbmFtZSA6PSByYW5nZSBuYW1lcyB7CgkJc291cmNlIDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkU3RhZ2luZywgbmFtZSkKCQl2ZXJpZnlSZWd1bGFyKHNvdXJjZSkKCQlzb3VyY2VEaWdlc3RzW25hbWVdID0gZGlnZXN0KHNvdXJjZSkKCX0KCXJlbW92ZVByb3RlY3RlZExlYXZlcyhwcm9maWxlKQoJZm9yIF8sIG5hbWUgOj0gcmFuZ2UgbmFtZXMgewoJCXNvdXJjZSA6PSBmaWxlcGF0aC5Kb2luKHByb3RlY3RlZFN0YWdpbmcsIG5hbWUpCgkJdGFyZ2V0IDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkVGFyZ2V0RGlyLCBuYW1lKQoJCWNvcHlQcm90ZWN0ZWQoc291cmNlLCB0YXJnZXQpCgkJdmVyaWZ5UmVndWxhcih0YXJnZXQpCgkJaWYgZGlnZXN0KHRhcmdldCkgIT0gc291cmNlRGlnZXN0c1tuYW1lXSB7CgkJCWZhaWwoInByb3RlY3RlZCBhcnRpZmFjdCBkaWdlc3QgbWlzbWF0Y2ggZm9yICVzIiwgbmFtZSkKCQl9Cgl9CglpZiBlcnIgOj0gb3MuUmVtb3ZlQWxsKHByb3RlY3RlZFN0YWdpbmcpOyBlcnIgIT0gbmlsIHsKCQlmYWlsKCJyZW1vdmUgcHJvdGVjdGVkIHN0YWdpbmc6ICV2IiwgZXJyKQoJfQoJdmVyaWZ5UGFyZW50cyhwcm90ZWN0ZWRUYXJnZXREaXIpCglmb3IgXywgbmFtZSA6PSByYW5nZSBuYW1lcyB7CgkJdGFyZ2V0IDo9IGZpbGVwYXRoLkpvaW4ocHJvdGVjdGVkVGFyZ2V0RGlyLCBuYW1lKQoJCXZlcmlmeVJlZ3VsYXIodGFyZ2V0KQoJCWlmIGRpZ2VzdCh0YXJnZXQpICE9IHNvdXJjZURpZ2VzdHNbbmFtZV0gewoJCQlmYWlsKCJmaW5hbCBwcm90ZWN0ZWQgYXJ0aWZhY3QgZGlnZXN0IG1pc21hdGNoIGZvciAlcyIsIG5hbWUpCgkJfQoJfQoJdmVyaWZ5UmVndWxhcihnb0V4ZWN1dGFibGUpCgl2ZXJpZnlHbyhnb0V4ZWN1dGFibGUpCn0KCmZ1bmMgbWFpbigpIHsKCWlmIGxlbihvcy5BcmdzKSA9PSAzICYmIG9zLkFyZ3NbMV0gPT0gInByZXBhcmUiIHsKCQlwcmVwYXJlKG9zLkFyZ3NbMl0pCgkJcmV0dXJuCgl9CglpZiBsZW4ob3MuQXJncykgPT0gNCAmJiBvcy5BcmdzWzFdID09ICJpbnN0YWxsIiB7CgkJaW5zdGFsbChvcy5BcmdzWzJdLCBvcy5BcmdzWzNdKQoJCXJldHVybgoJfQoJaWYgbGVuKG9zLkFyZ3MpID09IDMgJiYgb3MuQXJnc1sxXSA9PSAidmVyaWZ5IiB7CgkJdmVyaWZ5R28ob3MuQXJnc1syXSkKCQlyZXR1cm4KCX0KCWZhaWwoInVuc3VwcG9ydGVkIGludm9jYXRpb24iKQp9Cg=="
trusted_guard_build_run="RUN mkdir -p /out /tmp/mattercodex-go-build-cache && printf '%s' '$trusted_guard_source_base64' | base64 -d > /tmp/mattercodex-go-toolchain-guard.go && /usr/bin/env -i PATH=/usr/local/go/bin:/usr/bin:/bin HOME=/tmp GOCACHE=/tmp/mattercodex-go-build-cache GOENV=off GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 /usr/local/go/bin/go build -trimpath -buildvcs=false -o /out/mattercodex-go-toolchain-guard /tmp/mattercodex-go-toolchain-guard.go && rm -rf /tmp/mattercodex-go-toolchain-guard.go /tmp/mattercodex-go-build-cache"
require_line services/external/bot-service/Dockerfile "ARG GOLANG_IMAGE=$go_image"
require_final_runtime_stage_contract \
  services/jobs/agent-runner/Dockerfile \
  "FROM node:24-bookworm" \
  4 \
  2 \
  "COPY --from=1 /out/mattercodex-protected/ /usr/local/bin/" \
  "COPY --from=1 /out/mattercodex-protected/ /opt/mattercodex/protected-artifacts/" \
  4 \
  5 \
  "" \
  "ENV GOROOT=/usr/local/go GOENV=off GOFLAGS= GOTOOLCHAIN=local PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin PLAYWRIGHT_BROWSERS_PATH=/ms-playwright" \
  'USER 10001:10001' \
  'ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/matter-codex-agent-runner"]' \
  "aa16a5700a0367ec702edc2954d5c79274eab96f86511e92256e4e6a63a7c804"
require_final_runtime_stage_contract \
  deploy/images/agent-runner/Dockerfile \
  "FROM node:24-alpine" \
  2 \
  0 \
  "COPY --from=1 /out/mattercodex-protected/ /usr/local/bin/" \
  "COPY --from=1 /out/mattercodex-protected/ /opt/mattercodex/protected-artifacts/" \
  4 \
  5 \
  "WORKDIR /workspace" \
  "ENV GOROOT=/usr/local/go GOENV=off GOFLAGS= GOTOOLCHAIN=local PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin" \
  'USER 10001:10001' \
  'CMD ["sh"]' \
  "b6e7b6fb8a7b52891a9cfc39d2220975a9be8e7a056dd8cf6c58b51dadf302b4"
require_count services/jobs/agent-runner/Dockerfile "FROM $go_image" 2
require_count deploy/images/agent-runner/Dockerfile "FROM $go_image" 2
require_count services/external/bot-service/Dockerfile "ENV GOTOOLCHAIN=local" 2
require_count services/jobs/agent-runner/Dockerfile "ENV GOTOOLCHAIN=local" 2
require_count deploy/images/agent-runner/Dockerfile "ENV GOTOOLCHAIN=local" 2
require_count services/external/bot-service/Dockerfile "RUN test \"\$(go env GOVERSION)\" = \"go$go_version\"" 1
require_count services/jobs/agent-runner/Dockerfile "RUN test \"\$(go env GOVERSION)\" = \"go$go_version\"" 1
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

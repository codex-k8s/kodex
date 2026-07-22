#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
guard="$repo_root/scripts/check-go-toolchain.sh"
temp_root="$(mktemp -d)"
trap 'rm -rf "$temp_root"' EXIT

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

copy_fixture() {
  local destination="$1"
  local path

  for path in "${canonical_files[@]}"; do
    mkdir -p "$destination/$(dirname "$path")"
    cp "$repo_root/$path" "$destination/$path"
  done
}

prepend_lines() {
  local target="$1"
  shift

  printf '%s\n' "$@" >"$target.tmp"
  cat "$target" >>"$target.tmp"
  mv "$target.tmp" "$target"
}

replace_exact_instruction() {
  local target="$1"
  local expected="$2"
  local replacement="$3"

  LC_ALL=C awk -v expected="$expected" -v replacement="$replacement" '
    {
      if ($0 == expected) {
        selected++
        print replacement
      } else {
        print
      }
    }
    END {
      if (selected != 1) {
        exit 2
      }
    }
  ' "$target" >"$target.tmp"
  mv "$target.tmp" "$target"
}

insert_before_exact_instruction() {
  local target="$1"
  local expected="$2"
  local inserted="$3"

  LC_ALL=C awk -v expected="$expected" -v inserted="$inserted" '
    {
      if ($0 == expected) {
        selected++
        print inserted
      }
      print
    }
    END {
      if (selected != 1) {
        exit 2
      }
    }
  ' "$target" >"$target.tmp"
  mv "$target.tmp" "$target"
}

insert_after_exact_instruction() {
  local target="$1"
  local expected="$2"
  local inserted="$3"

  LC_ALL=C awk -v expected="$expected" -v inserted="$inserted" '
    {
      print
      if ($0 == expected) {
        selected++
        print inserted
      }
    }
    END {
      if (selected != 1) {
        exit 2
      }
    }
  ' "$target" >"$target.tmp"
  mv "$target.tmp" "$target"
}

absorb_exact_instruction() {
  local target="$1"
  local expected="$2"
  local actual="${3:-}"

  LC_ALL=C awk -v expected="$expected" -v actual="$actual" '
    {
      lines[NR] = $0
      if ($0 == expected) {
        selected = NR
      }
    }
    END {
      if (!selected) {
        exit 2
      }
      for (line_number = 1; line_number <= NR; line_number++) {
        if (line_number == selected) {
          print "RUN true \\"
        }
        print lines[line_number]
        if (line_number == selected && actual != "") {
          print actual
        }
      }
    }
  ' "$target" >"$target.tmp"
  mv "$target.tmp" "$target"
}

swap_exact_instructions() {
  local target="$1"
  local first="$2"
  local second="$3"

  LC_ALL=C awk -v first="$first" -v second="$second" '
    {
      lines[NR] = $0
      if ($0 == first) {
        first_selected = NR
      }
      if ($0 == second) {
        second_selected = NR
      }
    }
    END {
      if (!first_selected || !second_selected) {
        exit 2
      }
      for (line_number = 1; line_number <= NR; line_number++) {
        if (line_number == first_selected) {
          print second
        } else if (line_number == second_selected) {
          print first
        } else {
          print lines[line_number]
        }
      }
    }
  ' "$target" >"$target.tmp"
  mv "$target.tmp" "$target"
}

split_exact_instruction() {
  local target="$1"
  local expected="$2"
  local first_line="$3"
  local second_line="$4"

  LC_ALL=C awk \
    -v expected="$expected" \
    -v first_line="$first_line" \
    -v second_line="$second_line" '
      {
        lines[NR] = $0
        if ($0 == expected) {
          selected = NR
        }
      }
      END {
        if (!selected) {
          exit 2
        }
        for (line_number = 1; line_number <= NR; line_number++) {
          if (line_number == selected) {
            print first_line
            print second_line
          } else {
            print lines[line_number]
          }
        }
      }
    ' "$target" >"$target.tmp"
  mv "$target.tmp" "$target"
}

expect_failure() {
  local description="$1"
  shift

  if "$@" >"$temp_root/output" 2>&1; then
    echo "FAIL: $description не был отклонён" >&2
    exit 1
  fi
}

expect_failure_matching() {
  local description="$1"
  local expected="$2"
  local actual_status
  shift 2

  if "$@" >"$temp_root/output" 2>&1; then
    actual_status=0
  else
    actual_status=$?
  fi
  if [[ "$actual_status" == 0 ]]; then
    echo "FAIL: $description не был отклонён" >&2
    exit 1
  fi
  if [[ "$actual_status" != 1 ]]; then
    echo "FAIL: $description завершился с exit $actual_status вместо 1" >&2
    sed -n '1,80p' "$temp_root/output" >&2
    exit 1
  fi
  if ! grep -Fq "$expected" "$temp_root/output"; then
    echo "FAIL: $description отклонён не по ожидаемой причине" >&2
    sed -n '1,80p' "$temp_root/output" >&2
    exit 1
  fi
}

expect_success() {
  local description="$1"
  shift

  if ! "$@" >"$temp_root/output" 2>&1; then
    echo "FAIL: $description завершился ошибкой" >&2
    sed -n '1,80p' "$temp_root/output" >&2
    exit 1
  fi
}

expect_success \
  "canonical immutable source -> final COPY -> exact RUN test -> metadata tail" \
  "$guard" --root "$repo_root" --static-only

agent_runner_paths=(
  services/jobs/agent-runner/Dockerfile
  deploy/images/agent-runner/Dockerfile
)
agent_runner_names=(services deploy)
final_runtime_froms=('FROM node:24-bookworm' 'FROM node:24-alpine')
pinned_source_from='FROM golang:1.26.5-alpine AS go-toolchain-source'
pinned_go_tools_from='FROM go-toolchain-source AS go-tools'
runtime_env='ENV GOTOOLCHAIN=local'
runtime_copy='COPY --from=go-toolchain-source /usr/local/go /usr/local/go'
go_tools_copy='COPY --from=go-tools /tool-bin/ /usr/local/bin/'
runtime_check='RUN test "$(/usr/local/go/bin/go env GOVERSION)" = "go1.26.5" && test "$(/usr/local/go/bin/go env GOTOOLCHAIN)" = "local"'
fake_go_run='RUN echo IyEvYmluL3NoCmNhc2UgIiQqIiBpbgogICJlbnYgR09WRVJTSU9OIikgZWNobyBnbzEuMjYuNTs7CiAgImVudiBHT1RPT0xDSEFJTiIpIGVjaG8gbG9jYWw7OwogICopIGV4aXQgMDs7CmVzYWMK | base64 -d > /usr/local/go/bin/go && chmod +x /usr/local/go/bin/go'

for index in "${!agent_runner_paths[@]}"; do
  path="${agent_runner_paths[$index]}"
  name="${agent_runner_names[$index]}"
  final_runtime_from="${final_runtime_froms[$index]}"

  reviewer_alias_rebound="$temp_root/$name-reviewer-alias-rebound"
  copy_fixture "$reviewer_alias_rebound"
  replace_exact_instruction \
    "$reviewer_alias_rebound/$path" \
    "$pinned_go_tools_from" \
    'FROM golang:1.26.5-alpine AS pinned-go-tools'
  insert_before_exact_instruction \
    "$reviewer_alias_rebound/$path" \
    "$final_runtime_from" \
    'FROM attacker.invalid/go:latest AS go-tools'
  expect_failure_matching \
    "reviewer mutation перепривязывает alias go-tools в $name Dockerfile" \
    "$path: stage alias go-tools должна иметь точную привязку '$pinned_go_tools_from' к immutable source" \
    "$guard" --root "$reviewer_alias_rebound" --static-only

  security_alias_rebound="$temp_root/$name-security-alias-rebound"
  copy_fixture "$security_alias_rebound"
  replace_exact_instruction \
    "$security_alias_rebound/$path" \
    "$pinned_go_tools_from" \
    'FROM golang:1.26.5-alpine AS pinned-go-tools'
  insert_before_exact_instruction \
    "$security_alias_rebound/$path" \
    "$final_runtime_from" \
    'FROM example.invalid/untrusted/go-toolchain:latest AS go-tools'
  expect_failure_matching \
    "security mutation перепривязывает alias go-tools в $name Dockerfile" \
    "$path: stage alias go-tools должна иметь точную привязку '$pinned_go_tools_from' к immutable source" \
    "$guard" --root "$security_alias_rebound" --static-only

  renamed_go_tools_alias="$temp_root/$name-renamed-go-tools-alias"
  copy_fixture "$renamed_go_tools_alias"
  replace_exact_instruction \
    "$renamed_go_tools_alias/$path" \
    "$pinned_go_tools_from" \
    'FROM golang:1.26.5-alpine AS pinned-go-tools'
  expect_failure_matching \
    "переименование alias оставляет COPY с неявным внешним source в $name Dockerfile" \
    "$path: Dockerfile должен объявлять логическую stage alias go-tools ровно один раз; найдено 0" \
    "$guard" --root "$renamed_go_tools_alias" --static-only

  duplicate_go_tools_alias="$temp_root/$name-duplicate-go-tools-alias"
  copy_fixture "$duplicate_go_tools_alias"
  insert_before_exact_instruction \
    "$duplicate_go_tools_alias/$path" \
    "$final_runtime_from" \
    'FROM attacker.invalid/go:latest AS GO-TOOLS'
  expect_failure_matching \
    "alias go-tools продублирован с другим регистром в $name Dockerfile" \
    "$path: Dockerfile должен объявлять логическую stage alias go-tools ровно один раз; найдено 2" \
    "$guard" --root "$duplicate_go_tools_alias" --static-only

  absorbed_go_tools_stage="$temp_root/$name-absorbed-go-tools-stage"
  copy_fixture "$absorbed_go_tools_stage"
  absorb_exact_instruction "$absorbed_go_tools_stage/$path" "$pinned_go_tools_from"
  expect_failure_matching \
    "continuation поглощает объявление stage alias go-tools в $name Dockerfile" \
    "$path: immutable stage alias go-toolchain-source должна содержать только точную FROM-инструкцию" \
    "$guard" --root "$absorbed_go_tools_stage" --static-only

  variable_go_tools_base="$temp_root/$name-variable-go-tools-base"
  copy_fixture "$variable_go_tools_base"
  replace_exact_instruction \
    "$variable_go_tools_base/$path" \
    "$pinned_go_tools_from" \
    'FROM $GOLANG_IMAGE AS go-tools'
  expect_failure_matching \
    "переменный base stage alias go-tools в $name Dockerfile" \
    "$path: stage alias go-tools должна иметь точную привязку '$pinned_go_tools_from' к immutable source" \
    "$guard" --root "$variable_go_tools_base" --static-only

  flagged_go_tools_base="$temp_root/$name-flagged-go-tools-base"
  copy_fixture "$flagged_go_tools_base"
  replace_exact_instruction \
    "$flagged_go_tools_base/$path" \
    "$pinned_go_tools_from" \
    'FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine AS GO-TOOLS'
  expect_failure_matching \
    "FROM flag и другой регистр alias меняют каноническую stage в $name Dockerfile" \
    "$path: stage alias go-tools должна иметь точную привязку '$pinned_go_tools_from' к immutable source" \
    "$guard" --root "$flagged_go_tools_base" --static-only

  misplaced_go_tools_stage="$temp_root/$name-misplaced-go-tools-stage"
  copy_fixture "$misplaced_go_tools_stage"
  replace_exact_instruction \
    "$misplaced_go_tools_stage/$path" \
    "$pinned_go_tools_from" \
    'FROM golang:1.26.5-alpine AS pinned-go-tools'
  printf '\n%s\n' "$pinned_go_tools_from" >>"$misplaced_go_tools_stage/$path"
  expect_failure_matching \
    "stage alias go-tools объявлена после final runtime stage в $name Dockerfile" \
    "$path: immutable source, go-tools и final runtime stage должны быть объявлены в каноническом порядке" \
    "$guard" --root "$misplaced_go_tools_stage" --static-only

  mutated_immutable_source="$temp_root/$name-mutated-immutable-source"
  copy_fixture "$mutated_immutable_source"
  insert_after_exact_instruction \
    "$mutated_immutable_source/$path" \
    "$pinned_source_from" \
    'RUN rm -rf /usr/local/go'
  expect_failure_matching \
    "write-capable RUN изменяет immutable source stage в $name Dockerfile" \
    "$path: immutable stage alias go-toolchain-source должна содержать только точную FROM-инструкцию" \
    "$guard" --root "$mutated_immutable_source" --static-only

  mounted_immutable_source="$temp_root/$name-mounted-immutable-source"
  copy_fixture "$mounted_immutable_source"
  insert_after_exact_instruction \
    "$mounted_immutable_source/$path" \
    "$pinned_source_from" \
    'RUN --mount=type=bind,source=.,target=/mnt true'
  expect_failure_matching \
    "RUN mount нарушает immutable source stage в $name Dockerfile" \
    "$path: immutable stage alias go-toolchain-source должна содержать только точную FROM-инструкцию" \
    "$guard" --root "$mounted_immutable_source" --static-only

  duplicate_source_alias="$temp_root/$name-duplicate-source-alias"
  copy_fixture "$duplicate_source_alias"
  insert_before_exact_instruction \
    "$duplicate_source_alias/$path" \
    "$pinned_go_tools_from" \
    'FROM attacker.invalid/go:latest AS GO-TOOLCHAIN-SOURCE'
  expect_failure_matching \
    "immutable source alias продублирован с другим регистром в $name Dockerfile" \
    "$path: Dockerfile должен объявлять immutable stage alias go-toolchain-source ровно один раз; найдено 2" \
    "$guard" --root "$duplicate_source_alias" --static-only

  rebound_source_alias="$temp_root/$name-rebound-source-alias"
  copy_fixture "$rebound_source_alias"
  replace_exact_instruction \
    "$rebound_source_alias/$path" \
    "$pinned_source_from" \
    'FROM golang:1.26.5-alpine AS pinned-go-toolchain-source'
  insert_before_exact_instruction \
    "$rebound_source_alias/$path" \
    "$pinned_go_tools_from" \
    'FROM attacker.invalid/go:latest AS go-toolchain-source'
  expect_failure_matching \
    "immutable source alias перепривязан к внешнему image в $name Dockerfile" \
    "$path: immutable stage alias go-toolchain-source должна иметь точную закреплённую форму '$pinned_source_from'" \
    "$guard" --root "$rebound_source_alias" --static-only

  implicit_external_source="$temp_root/$name-implicit-external-source"
  copy_fixture "$implicit_external_source"
  replace_exact_instruction \
    "$implicit_external_source/$path" \
    "$pinned_source_from" \
    'FROM golang:1.26.5-alpine AS pinned-go-toolchain-source'
  expect_failure_matching \
    "отсутствующий source alias превращает FROM/COPY в implicit external source в $name Dockerfile" \
    "$path: Dockerfile должен объявлять immutable stage alias go-toolchain-source ровно один раз; найдено 0" \
    "$guard" --root "$implicit_external_source" --static-only

  fake_go_in_tools_stage="$temp_root/$name-fake-go-in-tools-stage"
  copy_fixture "$fake_go_in_tools_stage"
  insert_before_exact_instruction \
    "$fake_go_in_tools_stage/$path" \
    "$final_runtime_from" \
    "$fake_go_run"
  expect_failure_matching \
    "fake go executable добавлен внутри go-tools stage в $name Dockerfile" \
    "$path: stage alias go-tools должна содержать ровно две самостоятельные RUN-инструкции закреплённого tools-контракта; найдено 3" \
    "$guard" --root "$fake_go_in_tools_stage" --static-only

  external_copy_in_tools_stage="$temp_root/$name-external-copy-in-tools-stage"
  copy_fixture "$external_copy_in_tools_stage"
  insert_before_exact_instruction \
    "$external_copy_in_tools_stage/$path" \
    "$final_runtime_from" \
    'COPY --from=example.invalid/untrusted-go:latest /usr/local/go /usr/local/go'
  expect_failure_matching \
    "external COPY добавлен внутри go-tools stage в $name Dockerfile" \
    "$path: stage alias go-tools не должна содержать COPY или ADD; инструменты собираются только из закреплённого source" \
    "$guard" --root "$external_copy_in_tools_stage" --static-only

  absorbed_go_tools_copy="$temp_root/$name-absorbed-go-tools-copy"
  copy_fixture "$absorbed_go_tools_copy"
  absorb_exact_instruction "$absorbed_go_tools_copy/$path" "$go_tools_copy"
  expect_failure_matching \
    "continuation поглощает обязательный COPY Go tools из stage alias go-tools в $name Dockerfile" \
    "$path: final runtime stage должен копировать закреплённые Go tools одной точной самостоятельной логической COPY-инструкцией из stage alias go-tools" \
    "$guard" --root "$absorbed_go_tools_copy" --static-only

  absorbed_runtime_env="$temp_root/$name-absorbed-runtime-env"
  copy_fixture "$absorbed_runtime_env"
  absorb_exact_instruction \
    "$absorbed_runtime_env/$path" \
    "$runtime_env" \
    'ENV GOTOOLCHAIN="local"'
  expect_failure_matching \
    "continuation поглощает обязательный ENV в $name Dockerfile" \
    "$path: final runtime stage должен содержать точную самостоятельную логическую инструкцию '$runtime_env' ровно один раз" \
    "$guard" --root "$absorbed_runtime_env" --static-only

  absorbed_runtime_copy="$temp_root/$name-absorbed-runtime-copy"
  copy_fixture "$absorbed_runtime_copy"
  absorb_exact_instruction \
    "$absorbed_runtime_copy/$path" \
    "$runtime_copy" \
    'COPY --from=golang:latest /usr/local/go /usr/local/go'
  expect_failure_matching \
    "continuation поглощает закреплённый COPY и подменяет источник в $name Dockerfile" \
    "$path: final runtime stage содержит неразрешённую COPY-инструкцию с /usr/local/go" \
    "$guard" --root "$absorbed_runtime_copy" --static-only

  escaped_runtime_copy="$temp_root/$name-escaped-runtime-copy"
  copy_fixture "$escaped_runtime_copy"
  printf '%s\n' '' 'COPY --from=golang:latest /usr/local/g\o /usr/local/g\o' >>"$escaped_runtime_copy/$path"
  expect_failure_matching \
    "экранированный внешний COPY подменяет Go toolchain в $name Dockerfile" \
    "$path: final runtime stage содержит write-capable COPY после обязательного RUN test" \
    "$guard" --root "$escaped_runtime_copy" --static-only

  absorbed_runtime_check="$temp_root/$name-absorbed-runtime-check"
  copy_fixture "$absorbed_runtime_check"
  absorb_exact_instruction "$absorbed_runtime_check/$path" "$runtime_check"
  expect_failure_matching \
    "continuation поглощает обязательный RUN test в $name Dockerfile" \
    "$path: final runtime stage должен закрыто проверять точные GOVERSION и GOTOOLCHAIN одной самостоятельной логической RUN-инструкцией" \
    "$guard" --root "$absorbed_runtime_check" --static-only

  absorbed_runtime_contract="$temp_root/$name-absorbed-runtime-contract"
  copy_fixture "$absorbed_runtime_contract"
  absorb_exact_instruction \
    "$absorbed_runtime_contract/$path" \
    "$runtime_env" \
    'ENV GOTOOLCHAIN="local"'
  absorb_exact_instruction \
    "$absorbed_runtime_contract/$path" \
    "$runtime_copy" \
    'COPY --from=golang:latest /usr/local/go /usr/local/go'
  absorb_exact_instruction "$absorbed_runtime_contract/$path" "$runtime_check"
  expect_failure_matching \
    "continuation поглощает весь обязательный runtime-контракт в $name Dockerfile" \
    "$path: final runtime stage содержит неразрешённую COPY-инструкцию с /usr/local/go" \
    "$guard" --root "$absorbed_runtime_contract" --static-only

  runtime_env_after_copy="$temp_root/$name-runtime-env-after-copy"
  copy_fixture "$runtime_env_after_copy"
  swap_exact_instructions "$runtime_env_after_copy/$path" "$runtime_env" "$runtime_copy"
  expect_failure_matching \
    "логический ENV расположен после COPY в $name Dockerfile" \
    "$path: final runtime stage должен задавать GOTOOLCHAIN=local до логической COPY-инструкции Go toolchain" \
    "$guard" --root "$runtime_env_after_copy" --static-only

  runtime_check_before_copy="$temp_root/$name-runtime-check-before-copy"
  copy_fixture "$runtime_check_before_copy"
  swap_exact_instructions "$runtime_check_before_copy/$path" "$runtime_copy" "$runtime_check"
  expect_failure_matching \
    "логический RUN test расположен до COPY в $name Dockerfile" \
    "$path: final runtime stage должен проверять скопированный Go toolchain после логической COPY-инструкции" \
    "$guard" --root "$runtime_check_before_copy" --static-only

  shell_true_before_check="$temp_root/$name-shell-true-before-check"
  copy_fixture "$shell_true_before_check"
  insert_before_exact_instruction \
    "$shell_true_before_check/$path" \
    "$runtime_check" \
    'SHELL ["/bin/true"]'
  expect_failure_matching \
    "SHELL /bin/true нейтрализует exact RUN test в $name Dockerfile" \
    "$path: final runtime stage не должна содержать SHELL: обязательный RUN test использует только canonical shell" \
    "$guard" --root "$shell_true_before_check" --static-only

  alternate_shell_before_check="$temp_root/$name-alternate-shell-before-check"
  copy_fixture "$alternate_shell_before_check"
  insert_before_exact_instruction \
    "$alternate_shell_before_check/$path" \
    "$runtime_check" \
    'SHELL ["/bin/sh", "-c"]'
  expect_failure_matching \
    "альтернативный SHELL меняет семантику exact RUN test в $name Dockerfile" \
    "$path: final runtime stage не должна содержать SHELL: обязательный RUN test использует только canonical shell" \
    "$guard" --root "$alternate_shell_before_check" --static-only

  fake_go_between_copy_and_check="$temp_root/$name-fake-go-between-copy-and-check"
  copy_fixture "$fake_go_between_copy_and_check"
  insert_before_exact_instruction \
    "$fake_go_between_copy_and_check/$path" \
    "$runtime_check" \
    "$fake_go_run"
  expect_failure_matching \
    "fake go executable подменяет toolchain между trusted COPY и exact RUN test в $name Dockerfile" \
    "$path: final runtime stage содержит write-capable RUN между trusted COPY Go toolchain и exact RUN test" \
    "$guard" --root "$fake_go_between_copy_and_check" --static-only

  late_delete_after_check="$temp_root/$name-late-delete-after-check"
  copy_fixture "$late_delete_after_check"
  insert_after_exact_instruction \
    "$late_delete_after_check/$path" \
    "$runtime_check" \
    'RUN rm -rf /usr/local/go'
  expect_failure_matching \
    "поздний RUN удаляет Go toolchain после exact test в $name Dockerfile" \
    "$path: final runtime stage содержит write-capable RUN после обязательного RUN test" \
    "$guard" --root "$late_delete_after_check" --static-only

  late_replace_after_check="$temp_root/$name-late-replace-after-check"
  copy_fixture "$late_replace_after_check"
  insert_after_exact_instruction \
    "$late_replace_after_check/$path" \
    "$runtime_check" \
    "$fake_go_run"
  expect_failure_matching \
    "поздний RUN заменяет Go executable после exact test в $name Dockerfile" \
    "$path: final runtime stage содержит write-capable RUN после обязательного RUN test" \
    "$guard" --root "$late_replace_after_check" --static-only

  late_external_copy_after_check="$temp_root/$name-late-external-copy-after-check"
  copy_fixture "$late_external_copy_after_check"
  insert_after_exact_instruction \
    "$late_external_copy_after_check/$path" \
    "$runtime_check" \
    'COPY --from=example.invalid/untrusted-go:latest /usr/local/go /usr/local/go'
  expect_failure_matching \
    "поздний external COPY заменяет Go toolchain после exact test в $name Dockerfile" \
    "$path: final runtime stage содержит write-capable COPY после обязательного RUN test" \
    "$guard" --root "$late_external_copy_after_check" --static-only

  late_external_add_after_check="$temp_root/$name-late-external-add-after-check"
  copy_fixture "$late_external_add_after_check"
  insert_after_exact_instruction \
    "$late_external_add_after_check/$path" \
    "$runtime_check" \
    'ADD https://example.invalid/untrusted-go.tar /usr/local/go/'
  expect_failure_matching \
    "поздний external ADD заменяет Go toolchain после exact test в $name Dockerfile" \
    "$path: final runtime stage содержит write-capable ADD после обязательного RUN test" \
    "$guard" --root "$late_external_add_after_check" --static-only

  unlisted_metadata_after_check="$temp_root/$name-unlisted-metadata-after-check"
  copy_fixture "$unlisted_metadata_after_check"
  insert_after_exact_instruction \
    "$unlisted_metadata_after_check/$path" \
    "$runtime_check" \
    'LABEL security.proof=unlisted'
  expect_failure_matching \
    "незаявленная metadata-инструкция добавлена после exact test в $name Dockerfile" \
    "$path: после обязательного RUN test разрешён только точный metadata tail текущего Dockerfile" \
    "$guard" --root "$unlisted_metadata_after_check" --static-only

  duplicate_runtime_env="$temp_root/$name-duplicate-runtime-env"
  copy_fixture "$duplicate_runtime_env"
  printf '\n%s\n' "$runtime_env" >>"$duplicate_runtime_env/$path"
  expect_failure_matching \
    "обязательный логический ENV продублирован в $name Dockerfile" \
    "$path: final runtime stage должен содержать точную самостоятельную логическую инструкцию '$runtime_env' ровно один раз" \
    "$guard" --root "$duplicate_runtime_env" --static-only

  duplicate_runtime_copy="$temp_root/$name-duplicate-runtime-copy"
  copy_fixture "$duplicate_runtime_copy"
  printf '\n%s\n' "$runtime_copy" >>"$duplicate_runtime_copy/$path"
  expect_failure_matching \
    "обязательный логический COPY продублирован в $name Dockerfile" \
    "$path: final runtime stage должен копировать закреплённый Go toolchain одной точной самостоятельной логической COPY-инструкцией" \
    "$guard" --root "$duplicate_runtime_copy" --static-only

  duplicate_runtime_check="$temp_root/$name-duplicate-runtime-check"
  copy_fixture "$duplicate_runtime_check"
  printf '\n%s\n' "$runtime_check" >>"$duplicate_runtime_check/$path"
  expect_failure_matching \
    "обязательный логический RUN test продублирован в $name Dockerfile" \
    "$path: final runtime stage должен закрыто проверять точные GOVERSION и GOTOOLCHAIN одной самостоятельной логической RUN-инструкцией" \
    "$guard" --root "$duplicate_runtime_check" --static-only

  unicode_nbsp_from="$temp_root/$name-unicode-nbsp-from"
  copy_fixture "$unicode_nbsp_from"
  printf '\n\302\240FROM scratch\n' >>"$unicode_nbsp_from/$path"
  expect_failure_matching \
    "NBSP перед новой final stage в $name Dockerfile" \
    "недопустимый Unicode-пробел в Dockerfile" \
    "$guard" --root "$unicode_nbsp_from" --static-only

  unicode_em_space_from="$temp_root/$name-unicode-em-space-from"
  copy_fixture "$unicode_em_space_from"
  printf '\n\342\200\203FROM scratch\n' >>"$unicode_em_space_from/$path"
  expect_failure_matching \
    "EM SPACE перед новой final stage в $name Dockerfile" \
    "недопустимый Unicode-пробел в Dockerfile" \
    "$guard" --root "$unicode_em_space_from" --static-only

  unicode_nbsp_env="$temp_root/$name-unicode-nbsp-env"
  copy_fixture "$unicode_nbsp_env"
  printf '\n\302\240ENV GOTOOLCHAIN=auto\n' >>"$unicode_nbsp_env/$path"
  expect_failure_matching \
    "NBSP перед поздним GOTOOLCHAIN=auto в $name Dockerfile" \
    "недопустимый Unicode-пробел в Dockerfile" \
    "$guard" --root "$unicode_nbsp_env" --static-only

  modern_quoted_escape="$temp_root/$name-modern-quoted-escape"
  copy_fixture "$modern_quoted_escape"
  printf '%s\n' '' 'ENV GOTOOLCHAIN="loc\al"' >>"$modern_quoted_escape/$path"
  expect_failure_matching \
    "экранирование в кавычках современного ENV в $name Dockerfile" \
    "$path final runtime stage завершает GOTOOLCHAIN значением 'loc\al' вместо 'local'" \
    "$guard" --root "$modern_quoted_escape" --static-only

  legacy_quoted_escape="$temp_root/$name-legacy-quoted-escape"
  copy_fixture "$legacy_quoted_escape"
  printf '%s\n' '' 'ENV GOTOOLCHAIN "loc\al"' >>"$legacy_quoted_escape/$path"
  expect_failure_matching \
    "экранирование в кавычках устаревшего ENV в $name Dockerfile" \
    "$path final runtime stage завершает GOTOOLCHAIN значением 'loc\al' вместо 'local'" \
    "$guard" --root "$legacy_quoted_escape" --static-only

  control_whitespace_continuation="$temp_root/$name-control-whitespace-continuation"
  copy_fixture "$control_whitespace_continuation"
  printf '\nRUN true #\\\v\nENV GOTOOLCHAIN=auto\n' >>"$control_whitespace_continuation/$path"
  expect_failure_matching \
    "vertical tab после escape в $name Dockerfile" \
    "недопустимый управляющий ASCII-байт в Dockerfile" \
    "$guard" --root "$control_whitespace_continuation" --static-only

  bom_escape_override="$temp_root/$name-bom-escape-override"
  copy_fixture "$bom_escape_override"
  sed -i 's/\\$/`/' "$bom_escape_override/$path"
  {
    printf '\357\273\277# escape=`\n'
    cat "$bom_escape_override/$path"
    printf '\nRUN true #\\\nENV GOTOOLCHAIN=auto\n'
  } >"$bom_escape_override/$path.tmp"
  mv "$bom_escape_override/$path.tmp" "$bom_escape_override/$path"
  expect_failure_matching \
    "BOM перед backtick escape и скрытым поздним ENV в $name Dockerfile" \
    "UTF-8 BOM в Dockerfile не поддерживается" \
    "$guard" --root "$bom_escape_override" --static-only

  hash_syntax_frontend="$temp_root/$name-hash-syntax-frontend"
  copy_fixture "$hash_syntax_frontend"
  prepend_lines "$hash_syntax_frontend/$path" '# syntax=example.invalid/untrusted/frontend:latest'
  expect_failure_matching \
    "# syntax выбирает внешний frontend в $name Dockerfile" \
    "внешний Dockerfile syntax frontend не поддерживается" \
    "$guard" --root "$hash_syntax_frontend" --static-only

  slash_syntax_frontend="$temp_root/$name-slash-syntax-frontend"
  copy_fixture "$slash_syntax_frontend"
  prepend_lines "$slash_syntax_frontend/$path" '// syntax=example.invalid/untrusted/frontend:latest'
  expect_failure_matching \
    "// syntax выбирает внешний frontend в $name Dockerfile" \
    "внешний Dockerfile syntax frontend не поддерживается" \
    "$guard" --root "$slash_syntax_frontend" --static-only

  shebang_hash_syntax_frontend="$temp_root/$name-shebang-hash-syntax-frontend"
  copy_fixture "$shebang_hash_syntax_frontend"
  prepend_lines \
    "$shebang_hash_syntax_frontend/$path" \
    '#!/usr/bin/env dockerfile' \
    '# syntax=example.invalid/untrusted/frontend:latest'
  expect_failure_matching \
    "shebang перед # syntax выбирает внешний frontend в $name Dockerfile" \
    "внешний Dockerfile syntax frontend не поддерживается" \
    "$guard" --root "$shebang_hash_syntax_frontend" --static-only

  shebang_slash_syntax_frontend="$temp_root/$name-shebang-slash-syntax-frontend"
  copy_fixture "$shebang_slash_syntax_frontend"
  prepend_lines \
    "$shebang_slash_syntax_frontend/$path" \
    '#!/usr/bin/env dockerfile' \
    '// syntax=example.invalid/untrusted/frontend:latest'
  expect_failure_matching \
    "shebang перед // syntax выбирает внешний frontend в $name Dockerfile" \
    "внешний Dockerfile syntax frontend не поддерживается" \
    "$guard" --root "$shebang_slash_syntax_frontend" --static-only

  json_syntax_frontend="$temp_root/$name-json-syntax-frontend"
  copy_fixture "$json_syntax_frontend"
  printf '%s\n' '{"syntax":"example.invalid/untrusted/frontend:latest"}' >"$json_syntax_frontend/$path"
  expect_failure_matching \
    "JSON syntax выбирает внешний frontend в $name Dockerfile" \
    "внешний Dockerfile syntax frontend не поддерживается" \
    "$guard" --root "$json_syntax_frontend" --static-only

  shebang_json_syntax_frontend="$temp_root/$name-shebang-json-syntax-frontend"
  copy_fixture "$shebang_json_syntax_frontend"
  printf '%s\n' \
    '#!/usr/bin/env dockerfile' \
    '{"syntax":"example.invalid/untrusted/frontend:latest"}' \
    >"$shebang_json_syntax_frontend/$path"
  expect_failure_matching \
    "shebang перед JSON syntax выбирает внешний frontend в $name Dockerfile" \
    "внешний Dockerfile syntax frontend не поддерживается" \
    "$guard" --root "$shebang_json_syntax_frontend" --static-only

  bom_hash_syntax_frontend="$temp_root/$name-bom-hash-syntax-frontend"
  copy_fixture "$bom_hash_syntax_frontend"
  {
    printf '\357\273\277# syntax=example.invalid/untrusted/frontend:latest\n'
    cat "$bom_hash_syntax_frontend/$path"
  } >"$bom_hash_syntax_frontend/$path.tmp"
  mv "$bom_hash_syntax_frontend/$path.tmp" "$bom_hash_syntax_frontend/$path"
  expect_failure_matching \
    "BOM перед # syntax в $name Dockerfile" \
    "UTF-8 BOM в Dockerfile не поддерживается" \
    "$guard" --root "$bom_hash_syntax_frontend" --static-only
done

logical_contract_continuations="$temp_root/logical-contract-continuations"
copy_fixture "$logical_contract_continuations"
for path in "${agent_runner_paths[@]}"; do
  split_exact_instruction \
    "$logical_contract_continuations/$path" \
    "$runtime_copy" \
    'COPY --from=go-toolchain-source /usr/local/go \' \
    '/usr/local/go'
  split_exact_instruction \
    "$logical_contract_continuations/$path" \
    "$runtime_check" \
    'RUN test "$(/usr/local/go/bin/go env GOVERSION)" = "go1.26.5" && \' \
    'test "$(/usr/local/go/bin/go env GOTOOLCHAIN)" = "local"'
done
expect_success \
  "самостоятельные обязательные COPY и RUN поддерживают ordinary continuation" \
  "$guard" --root "$logical_contract_continuations" --static-only

allowed_shebang_comments="$temp_root/allowed-shebang-comments"
copy_fixture "$allowed_shebang_comments"
for path in "${agent_runner_paths[@]}"; do
  prepend_lines \
    "$allowed_shebang_comments/$path" \
    '#!/usr/bin/env dockerfile' \
    '# syntax is documented here without a parser directive' \
    '# syntax=example.invalid/ignored-after-ordinary-comment'
done
expect_success \
  "shebang и обычные комментарии не выбирают внешний frontend" \
  "$guard" --root "$allowed_shebang_comments" --static-only

ascii_indentation_local="$temp_root/ascii-indentation-local"
copy_fixture "$ascii_indentation_local"
for path in "${agent_runner_paths[@]}"; do
  insert_before_exact_instruction \
    "$ascii_indentation_local/$path" \
    "$runtime_copy" \
    $' \tENV GOTOOLCHAIN="local"'
done
expect_success \
  "ASCII-пробел и табуляция перед безопасным ENV до trusted COPY поддерживаются в обоих agent-runner Dockerfile" \
  "$guard" --root "$ascii_indentation_local" --static-only

ascii_continuation_suffix="$temp_root/ascii-continuation-suffix"
copy_fixture "$ascii_continuation_suffix"
for path in "${agent_runner_paths[@]}"; do
  insert_before_exact_instruction \
    "$ascii_continuation_suffix/$path" \
    "$runtime_copy" \
    $'ENV PATH=/usr/local/go/bin\\ \t\n    GOTOOLCHAIN="local"'
done
expect_success \
  "ASCII-пробел и табуляция после escape сохраняют Dockerfile continuation" \
  "$guard" --root "$ascii_continuation_suffix" --static-only

modern_quoted_local="$temp_root/modern-quoted-local"
copy_fixture "$modern_quoted_local"
for path in "${agent_runner_paths[@]}"; do
  insert_before_exact_instruction \
    "$modern_quoted_local/$path" \
    "$runtime_copy" \
    'ENV GOTOOLCHAIN="local"'
done
expect_success \
  "local в кавычках современного ENV поддерживается в обоих agent-runner Dockerfile" \
  "$guard" --root "$modern_quoted_local" --static-only

legacy_quoted_local="$temp_root/legacy-quoted-local"
copy_fixture "$legacy_quoted_local"
for path in "${agent_runner_paths[@]}"; do
  insert_before_exact_instruction \
    "$legacy_quoted_local/$path" \
    "$runtime_copy" \
    'ENV GOTOOLCHAIN "local"'
done
expect_success \
  "local в кавычках устаревшего ENV поддерживается в обоих agent-runner Dockerfile" \
  "$guard" --root "$legacy_quoted_local" --static-only

below_floor="$temp_root/below-floor"
copy_fixture "$below_floor"
sed -i 's/^go 1\.26\.5$/go 1.26.4/' "$below_floor/go.mod"
expect_failure "go.mod ниже security floor" "$guard" --root "$below_floor" --static-only

partial_bump="$temp_root/partial-bump"
copy_fixture "$partial_bump"
sed -i 's/^go 1\.26\.5$/go 1.26.6/' "$partial_bump/go.mod"
expect_failure "частичный будущий bump" "$guard" --root "$partial_bump" --static-only

floating_image="$temp_root/floating-image"
copy_fixture "$floating_image"
sed -i '0,/golang:1\.26\.5-alpine/s//golang:1.26-alpine/' "$floating_image/services/jobs/agent-runner/Dockerfile"
expect_failure "плавающий Go image" "$guard" --root "$floating_image" --static-only

runtime_env_missing="$temp_root/runtime-env-missing"
copy_fixture "$runtime_env_missing"
sed -i '/^FROM node:24-bookworm$/,$ { /^ENV GOTOOLCHAIN=local$/d; }' "$runtime_env_missing/services/jobs/agent-runner/Dockerfile"
expect_failure "GOTOOLCHAIN=local отсутствует в services final runtime stage" "$guard" --root "$runtime_env_missing" --static-only

runtime_check_missing="$temp_root/runtime-check-missing"
copy_fixture "$runtime_check_missing"
sed -i '/^RUN test "$(\/usr\/local\/go\/bin\/go env GOVERSION)" = "go1\.26\.5" && test "$(\/usr\/local\/go\/bin\/go env GOTOOLCHAIN)" = "local"$/d' "$runtime_check_missing/deploy/images/agent-runner/Dockerfile"
expect_failure "точная проверка Go отсутствует в deploy final runtime stage" "$guard" --root "$runtime_check_missing" --static-only

runtime_final_local="$temp_root/runtime-final-local"
copy_fixture "$runtime_final_local"
for path in "${agent_runner_paths[@]}"; do
  insert_before_exact_instruction \
    "$runtime_final_local/$path" \
    "$runtime_copy" \
    $'ENV GOTOOLCHAIN=auto\nENV PATH=/usr/local/go/bin:/usr/local/bin \\\n    GOTOOLCHAIN="local"'
done
expect_success "последний GOTOOLCHAIN=local определяет эффективное значение" "$guard" --root "$runtime_final_local" --static-only

legacy_runtime_final_local="$temp_root/legacy-runtime-final-local"
copy_fixture "$legacy_runtime_final_local"
insert_before_exact_instruction \
  "$legacy_runtime_final_local/services/jobs/agent-runner/Dockerfile" \
  "$runtime_copy" \
  $'ENV GOTOOLCHAIN auto\nENV GOTOOLCHAIN local'
expect_success "устаревший ENV сохраняет итоговый GOTOOLCHAIN=local" "$guard" --root "$legacy_runtime_final_local" --static-only

services_runtime_override="$temp_root/services-runtime-override"
copy_fixture "$services_runtime_override"
printf '\nENV GOTOOLCHAIN=auto\n' >>"$services_runtime_override/services/jobs/agent-runner/Dockerfile"
expect_failure_matching \
  "поздний GOTOOLCHAIN=auto в services final runtime stage" \
  "services/jobs/agent-runner/Dockerfile final runtime stage завершает GOTOOLCHAIN значением 'auto' вместо 'local'" \
  "$guard" --root "$services_runtime_override" --static-only

deploy_runtime_override="$temp_root/deploy-runtime-override"
copy_fixture "$deploy_runtime_override"
printf '\nENV GOTOOLCHAIN=auto\n' >>"$deploy_runtime_override/deploy/images/agent-runner/Dockerfile"
expect_failure_matching \
  "поздний GOTOOLCHAIN=auto в deploy final runtime stage" \
  "deploy/images/agent-runner/Dockerfile final runtime stage завершает GOTOOLCHAIN значением 'auto' вместо 'local'" \
  "$guard" --root "$deploy_runtime_override" --static-only

services_multiline_override="$temp_root/services-multiline-override"
copy_fixture "$services_multiline_override"
cat >>"$services_multiline_override/services/jobs/agent-runner/Dockerfile" <<'EOF'

ENV PATH=/usr/local/go/bin:/usr/local/bin \
    GOTOOLCHAIN=auto
EOF
expect_failure_matching \
  "многострочный GOTOOLCHAIN=auto в services final runtime stage" \
  "services/jobs/agent-runner/Dockerfile final runtime stage завершает GOTOOLCHAIN значением 'auto' вместо 'local'" \
  "$guard" --root "$services_multiline_override" --static-only

deploy_multiline_override="$temp_root/deploy-multiline-override"
copy_fixture "$deploy_multiline_override"
cat >>"$deploy_multiline_override/deploy/images/agent-runner/Dockerfile" <<'EOF'

ENV PATH=/usr/local/go/bin:/usr/local/bin \
    GOTOOLCHAIN=auto
EOF
expect_failure_matching \
  "многострочный GOTOOLCHAIN=auto в deploy final runtime stage" \
  "deploy/images/agent-runner/Dockerfile final runtime stage завершает GOTOOLCHAIN значением 'auto' вместо 'local'" \
  "$guard" --root "$deploy_multiline_override" --static-only

ambiguous_runtime_env="$temp_root/ambiguous-runtime-env"
copy_fixture "$ambiguous_runtime_env"
printf '\nENV PATH=/usr/local/bin GOTOOLCHAIN\n' >>"$ambiguous_runtime_env/services/jobs/agent-runner/Dockerfile"
expect_failure_matching \
  "неоднозначный ENV-синтаксис с GOTOOLCHAIN" \
  "services/jobs/agent-runner/Dockerfile final runtime stage содержит неоднозначный ENV-синтаксис" \
  "$guard" --root "$ambiguous_runtime_env" --static-only

services_lowercase_final_from="$temp_root/services-lowercase-final-from"
copy_fixture "$services_lowercase_final_from"
printf '\nfrom scratch\n' >>"$services_lowercase_final_from/services/jobs/agent-runner/Dockerfile"
expect_failure_matching \
  "строчная новая final stage в services Dockerfile" \
  "services/jobs/agent-runner/Dockerfile должен завершаться stage 'FROM node:24-bookworm', найден 'FROM scratch'" \
  "$guard" --root "$services_lowercase_final_from" --static-only

deploy_indented_final_from="$temp_root/deploy-indented-final-from"
copy_fixture "$deploy_indented_final_from"
printf '\n  FROM scratch\n' >>"$deploy_indented_final_from/deploy/images/agent-runner/Dockerfile"
expect_failure_matching \
  "новая final stage с отступом в deploy Dockerfile" \
  "deploy/images/agent-runner/Dockerfile должен завершаться stage 'FROM node:24-alpine', найден 'FROM scratch'" \
  "$guard" --root "$deploy_indented_final_from" --static-only

services_three_slash_final_from="$temp_root/services-three-slash-final-from"
copy_fixture "$services_three_slash_final_from"
cat >>"$services_three_slash_final_from/services/jobs/agent-runner/Dockerfile" <<'EOF'

RUN true \\\
FROM scratch
EOF
expect_failure_matching \
  "три завершающих backslash перед новой final stage в services Dockerfile" \
  "services/jobs/agent-runner/Dockerfile должен завершаться stage 'FROM node:24-bookworm', найден 'FROM scratch'" \
  "$guard" --root "$services_three_slash_final_from" --static-only

deploy_three_slash_final_from="$temp_root/deploy-three-slash-final-from"
copy_fixture "$deploy_three_slash_final_from"
cat >>"$deploy_three_slash_final_from/deploy/images/agent-runner/Dockerfile" <<'EOF'

RUN true \\\
FROM scratch
EOF
expect_failure_matching \
  "три завершающих backslash перед новой final stage в deploy Dockerfile" \
  "deploy/images/agent-runner/Dockerfile должен завершаться stage 'FROM node:24-alpine', найден 'FROM scratch'" \
  "$guard" --root "$deploy_three_slash_final_from" --static-only

services_split_instruction="$temp_root/services-split-instruction"
copy_fixture "$services_split_instruction"
cat >>"$services_split_instruction/services/jobs/agent-runner/Dockerfile" <<'EOF'

EN\
# строка комментария внутри продолжения
V GOTOOLCHAIN=auto
EOF
expect_failure_matching \
  "разрыв имени ENV-инструкции в services Dockerfile" \
  "services/jobs/agent-runner/Dockerfile final runtime stage завершает GOTOOLCHAIN значением 'auto' вместо 'local'" \
  "$guard" --root "$services_split_instruction" --static-only

services_split_key="$temp_root/services-split-key"
copy_fixture "$services_split_key"
cat >>"$services_split_key/services/jobs/agent-runner/Dockerfile" <<'EOF'

ENV GOTOO\
LCHAIN=auto
EOF
expect_failure_matching \
  "разрыв имени GOTOOLCHAIN в services Dockerfile" \
  "services/jobs/agent-runner/Dockerfile final runtime stage завершает GOTOOLCHAIN значением 'auto' вместо 'local'" \
  "$guard" --root "$services_split_key" --static-only

deploy_split_instruction="$temp_root/deploy-split-instruction"
copy_fixture "$deploy_split_instruction"
cat >>"$deploy_split_instruction/deploy/images/agent-runner/Dockerfile" <<'EOF'

EN\
V GOTOOLCHAIN=auto
EOF
expect_failure_matching \
  "разрыв имени ENV-инструкции в deploy Dockerfile" \
  "deploy/images/agent-runner/Dockerfile final runtime stage завершает GOTOOLCHAIN значением 'auto' вместо 'local'" \
  "$guard" --root "$deploy_split_instruction" --static-only

deploy_split_key="$temp_root/deploy-split-key"
copy_fixture "$deploy_split_key"
cat >>"$deploy_split_key/deploy/images/agent-runner/Dockerfile" <<'EOF'

ENV GOTOO\
LCHAIN=auto
EOF
expect_failure_matching \
  "разрыв имени GOTOOLCHAIN в deploy Dockerfile" \
  "deploy/images/agent-runner/Dockerfile final runtime stage завершает GOTOOLCHAIN значением 'auto' вместо 'local'" \
  "$guard" --root "$deploy_split_key" --static-only

services_three_slash_override="$temp_root/services-three-slash-override"
copy_fixture "$services_three_slash_override"
cat >>"$services_three_slash_override/services/jobs/agent-runner/Dockerfile" <<'EOF'

RUN true \\\
ENV GOTOOLCHAIN=auto
EOF
expect_failure_matching \
  "три завершающих backslash перед поздним GOTOOLCHAIN=auto в services Dockerfile" \
  "services/jobs/agent-runner/Dockerfile: final runtime stage содержит write-capable RUN после обязательного RUN test" \
  "$guard" --root "$services_three_slash_override" --static-only

deploy_three_slash_override="$temp_root/deploy-three-slash-override"
copy_fixture "$deploy_three_slash_override"
cat >>"$deploy_three_slash_override/deploy/images/agent-runner/Dockerfile" <<'EOF'

RUN true \\\
ENV GOTOOLCHAIN=auto
EOF
expect_failure_matching \
  "три завершающих backslash перед поздним GOTOOLCHAIN=auto в deploy Dockerfile" \
  "deploy/images/agent-runner/Dockerfile: final runtime stage содержит write-capable RUN после обязательного RUN test" \
  "$guard" --root "$deploy_three_slash_override" --static-only

services_heredoc="$temp_root/services-heredoc"
copy_fixture "$services_heredoc"
cat >>"$services_heredoc/services/jobs/agent-runner/Dockerfile" <<'EOF'

ENV GOTOOLCHAIN=auto
COPY <<EOF_HEREDOC /tmp/gotoolchain-proof
FROM scratch
ENV GOTOOLCHAIN="local"
EOF_HEREDOC
EOF
expect_failure_matching \
  "heredoc с ложными FROM и ENV в services Dockerfile" \
  "Dockerfile heredoc не поддерживается проверкой final runtime stage" \
  "$guard" --root "$services_heredoc" --static-only

deploy_heredoc="$temp_root/deploy-heredoc"
copy_fixture "$deploy_heredoc"
cat >>"$deploy_heredoc/deploy/images/agent-runner/Dockerfile" <<'EOF'

ENV GOTOOLCHAIN=auto
COPY <<EOF_HEREDOC /tmp/gotoolchain-proof
FROM scratch
ENV GOTOOLCHAIN="local"
EOF_HEREDOC
EOF
expect_failure_matching \
  "heredoc с ложными FROM и ENV в deploy Dockerfile" \
  "Dockerfile heredoc не поддерживается проверкой final runtime stage" \
  "$guard" --root "$deploy_heredoc" --static-only

escape_parser_directive="$temp_root/escape-parser-directive"
copy_fixture "$escape_parser_directive"
sed -i '1i# escape=`' "$escape_parser_directive/services/jobs/agent-runner/Dockerfile"
expect_failure_matching \
  "нестандартный Dockerfile escape parser directive" \
  "Dockerfile parser directive escape не поддерживается проверкой final runtime stage" \
  "$guard" --root "$escape_parser_directive" --static-only

govulncheck_version_desync="$temp_root/govulncheck-version-desync"
copy_fixture "$govulncheck_version_desync"
sed -i 's/^GOVULNCHECK_VERSION := v1\.6\.0$/GOVULNCHECK_VERSION := v1.6.1/' "$govulncheck_version_desync/Makefile"
expect_failure "версия govulncheck рассинхронизирована с каталогом" "$guard" --root "$govulncheck_version_desync" --static-only

govulncheck_validation_missing="$temp_root/govulncheck-validation-missing"
copy_fixture "$govulncheck_validation_missing"
sed -i '/origin GOVULNCHECK_VERSION/d' "$govulncheck_validation_missing/Makefile"
expect_failure "Make override govulncheck не отклоняется" "$guard" --root "$govulncheck_validation_missing" --static-only

govulncheck_argument_unquoted="$temp_root/govulncheck-argument-unquoted"
copy_fixture "$govulncheck_argument_unquoted"
sed -i "/govulncheck@/s/'//g" "$govulncheck_argument_unquoted/Makefile"
expect_failure "module@version govulncheck передаётся без безопасных кавычек" "$guard" --root "$govulncheck_argument_unquoted" --static-only

fake_bin="$temp_root/fake-bin"
mkdir -p "$fake_bin"
cat >"$fake_bin/go" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "env" && "${2:-}" == "GOVERSION" ]]; then
  echo "go1.26.4"
  exit 0
fi
exit 2
EOF
chmod +x "$fake_bin/go"
expect_failure "runtime toolchain ниже 1.26.5" env PATH="$fake_bin:$PATH" "$guard" --root "$repo_root"

govulncheck_fake_bin="$temp_root/govulncheck-fake-bin"
govulncheck_marker="$temp_root/govulncheck-started"
mkdir -p "$govulncheck_fake_bin"
cat >"$govulncheck_fake_bin/go" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "env" && "${2:-}" == "GOVERSION" ]]; then
  echo "go1.26.5"
  exit 0
fi
if [[ "${1:-}" == "run" ]]; then
  : >"$GOVULNCHECK_TEST_MARKER"
  exit 0
fi
exit 2
EOF
chmod +x "$govulncheck_fake_bin/go"
injection_payload='v1.6.0 -h >/dev/null 2>&1; : #'
expect_failure "GOVULNCHECK_VERSION command injection" env \
  PATH="$govulncheck_fake_bin:$PATH" \
  GOVULNCHECK_TEST_MARKER="$govulncheck_marker" \
  make -C "$repo_root" GOVULNCHECK_VERSION="$injection_payload" govulncheck
[[ ! -e "$govulncheck_marker" ]] || {
  echo "FAIL: govulncheck был запущен после небезопасного Make override" >&2
  exit 1
}

echo "PASS: Go toolchain contract отклоняет старую версию, подмену provenance, SHELL, post-test mutation и govulncheck injection"

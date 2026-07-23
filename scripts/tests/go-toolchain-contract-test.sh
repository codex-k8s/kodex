#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
guard="$repo_root/scripts/check-go-toolchain.sh"
temp_root="$(mktemp -d)"
trap 'rm -rf "$temp_root"' EXIT

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

replace_nth_exact_instruction() {
  local target="$1"
  local expected="$2"
  local occurrence="$3"
  local replacement="$4"

  LC_ALL=C awk -v expected="$expected" -v occurrence="$occurrence" -v replacement="$replacement" '
    {
      if ($0 == expected) {
        selected++
        if (selected == occurrence) {
          print replacement
          next
        }
      }
      print
    }
    END {
      if (selected < occurrence) {
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

insert_after_nth_exact_instruction() {
  local target="$1"
  local expected="$2"
  local occurrence="$3"
  local inserted="$4"

  LC_ALL=C awk -v expected="$expected" -v occurrence="$occurrence" -v inserted="$inserted" '
    {
      print
      if ($0 == expected) {
        selected++
        if (selected == occurrence) {
          print inserted
          inserted_once = 1
        }
      }
    }
    END {
      if (!inserted_once) {
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

poison_installer_run() {
  local target="$1"
  local payload="$2"

  LC_ALL=C awk -v payload="$payload" '
    {
      if ($0 ~ /^[ \t]+&& CGO_ENABLED=0 go install go[.]uber[.]org\/mock\/mockgen@/) {
        selected++
        match($0, /^[ \t]*/)
        indentation = substr($0, 1, RLENGTH)
        print $0 " \\"
        print indentation "&& printf \047%s\047 \047" payload "\047 | base64 -d > /usr/local/go/bin/go \\"
        print indentation "&& chmod +x /usr/local/go/bin/go"
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
  if ! grep -Fq -- "$expected" "$temp_root/output"; then
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
  "canonical protected stages -> Kaniko bootstrap -> trusted clean/copy/verify" \
  "$guard" --root "$repo_root" --static-only

agent_runner_paths=(
  services/jobs/agent-runner/Dockerfile
  deploy/images/agent-runner/Dockerfile
)
agent_runner_names=(services deploy)
final_runtime_froms=('FROM node:24-bookworm' 'FROM node:24-alpine')
source_stage='FROM golang:1.26.5-alpine AS scratch'
work_stage='FROM golang:1.26.5-alpine AS context'
runtime_env='ENV GOTOOLCHAIN=local'
final_runtime_envs=(
  'ENV GOROOT=/usr/local/go GOENV=off GOFLAGS= GOTOOLCHAIN=local PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin PLAYWRIGHT_BROWSERS_PATH=/ms-playwright'
  'ENV GOROOT=/usr/local/go GOENV=off GOFLAGS= GOTOOLCHAIN=local PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin'
)
bootstrap_copy='COPY --from=0 /usr/local/go/ /opt/mattercodex/bootstrap-go/'
guard_copy='COPY --from=0 /out/mattercodex-go-toolchain-guard /usr/local/libexec/mattercodex-go-toolchain-guard'
clean_run='RUN ["/usr/local/libexec/mattercodex-go-toolchain-guard", "clean"]'
runtime_copy='COPY --from=0 /usr/local/go/ /usr/local/go/'
verify_run='RUN ["/usr/local/libexec/mattercodex-go-toolchain-guard", "verify", "/usr/local/go/bin/go"]'
go_tools_copies=('COPY --from=1 /tool-bin/ /usr/local/bin/' 'COPY --from=1 /tool-bin/ /usr/local/bin/')
goose_version_lines=($'\tgoose --version >/dev/null; \\' $'  && goose --version >/dev/null \\')
goose_mutation_lines=($'\tcp /bin/true /usr/local/bin/goose; \\' $'  && cp /bin/true /usr/local/bin/goose \\')
staticcheck_version_lines=($'\tstaticcheck -version >/dev/null; \\' $'  && staticcheck -version >/dev/null \\')
staticcheck_mutation_lines=($'\tcp /bin/true /usr/local/bin/staticcheck; \\' $'  && cp /bin/true /usr/local/bin/staticcheck \\')
fake_go_run='RUN echo IyEvYmluL3NoCmNhc2UgIiQqIiBpbgogICJlbnYgR09WRVJTSU9OIikgZWNobyBnbzEuMjYuNTs7CiAgImVudiBHT1RPT0xDSEFJTiIpIGVjaG8gbG9jYWw7OwogICopIGV4aXQgMDs7CmVzYWMK | base64 -d > /usr/local/go/bin/go && chmod +x /usr/local/go/bin/go'
destination_only_run="RUN printf 'package runtime\\nvar matterCodexInjected = true\\n' > /usr/local/go/src/runtime/mattercodex_injected.go"
fake_compiler_payloads=(
  'IyEvYmluL3NoCm91dD0Kd2hpbGUgWyAiJCMiIC1ndCAwIF07IGRvCiAgaWYgWyAiJDEiID0gIi1vIiBdOyB0aGVuIHNoaWZ0OyBvdXQ9IiQxIjsgYnJlYWs7IGZpCiAgc2hpZnQKZG9uZQpbIC1uICIkb3V0IiBdIHx8IGV4aXQgMgpjcCAvYmluL3RydWUgIiRvdXQiCg=='
  'IyEvYmluL3NoCm91dD0Kd2hpbGUgWyAiJCMiIC1ndCAwIF07IGRvCiAgaWYgWyAiJDEiID0gIi1vIiBdOyB0aGVuIHNoaWZ0OyBvdXQ9IiQxIjsgYnJlYWs7IGZpCiAgc2hpZnQKZG9uZQpbIC1uICIkb3V0IiBdIHx8IGV4aXQgMgpwcmludGYgIiMhL2Jpbi9zaFxcbmV4aXQgMFxcbiIgPiAiJG91dCIKY2htb2QgK3ggIiRvdXQiCg=='
)
fake_compiler_names=(reviewer security)

for index in "${!agent_runner_paths[@]}"; do
  path="${agent_runner_paths[$index]}"
  name="${agent_runner_names[$index]}"
  final_runtime_from="${final_runtime_froms[$index]}"
  final_runtime_env="${final_runtime_envs[$index]}"

  go_tools_copy="${go_tools_copies[$index]}"

  poisoned_final_goose="$temp_root/$name-poisoned-final-goose"
  copy_fixture "$poisoned_final_goose"
  insert_after_exact_instruction \
    "$poisoned_final_goose/$path" \
    "${goose_version_lines[$index]}" \
    "${goose_mutation_lines[$index]}"
  expect_success \
    "final trusted COPY восстанавливает goose после подмены внутри существующего installer RUN в $name Dockerfile" \
    "$guard" --root "$poisoned_final_goose" --static-only
  expect_success \
    "full guard подтверждает безопасный final COPY после подмены goose внутри installer RUN в $name Dockerfile" \
    "$guard" --root "$poisoned_final_goose"

  poisoned_final_staticcheck="$temp_root/$name-poisoned-final-staticcheck"
  copy_fixture "$poisoned_final_staticcheck"
  insert_after_exact_instruction \
    "$poisoned_final_staticcheck/$path" \
    "${staticcheck_version_lines[$index]}" \
    "${staticcheck_mutation_lines[$index]}"
  expect_success \
    "final trusted COPY восстанавливает staticcheck после подмены внутри существующего installer RUN в $name Dockerfile" \
    "$guard" --root "$poisoned_final_staticcheck" --static-only

  duplicate_final_tools_copy="$temp_root/$name-duplicate-final-tools-copy"
  copy_fixture "$duplicate_final_tools_copy"
  insert_before_exact_instruction "$duplicate_final_tools_copy/$path" "$guard_copy" "$go_tools_copy"
  expect_failure_matching \
    "final COPY Go tools продублирован в $name Dockerfile" \
    "$path: final runtime stage должен дважды копировать закреплённые Go tools" \
    "$guard" --root "$duplicate_final_tools_copy" --static-only

  substituted_final_tools_source="$temp_root/$name-substituted-final-tools-source"
  copy_fixture "$substituted_final_tools_source"
  replace_nth_exact_instruction \
    "$substituted_final_tools_source/$path" \
    "$go_tools_copy" \
    2 \
    'COPY --from=0 /tool-bin/ /usr/local/bin/'
  expect_failure_matching \
    "final COPY Go tools получает подменённый numeric source в $name Dockerfile" \
    "$path: final runtime stage содержит неразрешённую COPY-инструкцию вне точного списка разрешённых источников" \
    "$guard" --root "$substituted_final_tools_source" --static-only

  reordered_final_tools_copy="$temp_root/$name-reordered-final-tools-copy"
  copy_fixture "$reordered_final_tools_copy"
  swap_exact_instructions "$reordered_final_tools_copy/$path" "$go_tools_copy" "$guard_copy"
  expect_failure_matching \
    "final COPY Go tools переставлен после trusted guard COPY в $name Dockerfile" \
    "$path: final Go tools COPY, trusted guard COPY, clean, final Go COPY, final ENV и verification должны идти непосредственно друг за другом" \
    "$guard" --root "$reordered_final_tools_copy" --static-only

  for env_case in \
    'ENV GOROOT=/opt/mattercodex/attacker-go' \
    'ENV GOROOT /opt/mattercodex/attacker-go' \
    'ENV GOROOT="/opt/mattercodex/attacker-go"' \
    $'ENV PATH=/usr/local/bin \\\n    GOROOT=/opt/mattercodex/attacker-go' \
    'ENV GOROOT=/usr/local/go'; do
    env_case_key="$(printf '%s' "$env_case" | sha256sum | cut -c1-12)"
    inherited_goroot="$temp_root/$name-inherited-goroot-$env_case_key"
    copy_fixture "$inherited_goroot"
    insert_after_nth_exact_instruction "$inherited_goroot/$path" "$go_tools_copy" 1 "$env_case"
    expect_failure_matching \
      "дополнительный modern/legacy/quoted/continuation/duplicate GOROOT отклонён в $name Dockerfile" \
      "$path: final runtime stage должен задавать GOROOT=/usr/local/go ровно один раз в final ENV" \
      "$guard" --root "$inherited_goroot" --static-only
  done

  inherited_workdir="$temp_root/$name-inherited-workdir"
  copy_fixture "$inherited_workdir"
  insert_after_nth_exact_instruction \
    "$inherited_workdir/$path" \
    "$go_tools_copy" \
    1 \
    $'WORKDIR /opt/mattercodex/attacker-go'
  expect_failure_matching \
    "дополнительный WORKDIR attacker отклонён в $name Dockerfile" \
    "$path: final runtime stage" \
    "$guard" --root "$inherited_workdir" --static-only

  late_goroot="$temp_root/$name-late-goroot"
  copy_fixture "$late_goroot"
  printf '\nENV GOROOT=/opt/mattercodex/attacker-go\n' >>"$late_goroot/$path"
  expect_failure_matching \
    "поздний GOROOT override отклонён в $name Dockerfile" \
    "$path: final runtime stage должен задавать GOROOT=/usr/local/go ровно один раз в final ENV" \
    "$guard" --root "$late_goroot" --static-only

  late_workdir="$temp_root/$name-late-workdir"
  copy_fixture "$late_workdir"
  printf '\nWORKDIR /opt/mattercodex/attacker-go\n' >>"$late_workdir/$path"
  expect_failure_matching \
    "поздний WORKDIR attacker отклонён в $name Dockerfile" \
    "$path: final runtime stage" \
    "$guard" --root "$late_workdir" --static-only

  for selection_override in 'ENV GOENV=/tmp/attacker-goenv' 'ENV GOFLAGS=-toolexec=/tmp/attacker'; do
    override_key="$(printf '%s' "$selection_override" | sha256sum | cut -c1-12)"
    inherited_selection_override="$temp_root/$name-inherited-selection-$override_key"
    copy_fixture "$inherited_selection_override"
    insert_after_nth_exact_instruction "$inherited_selection_override/$path" "$go_tools_copy" 1 "$selection_override"
    expect_failure_matching \
      "дополнительный GOENV/GOFLAGS selection override отклонён в $name Dockerfile" \
      "$path: final runtime stage должен" \
      "$guard" --root "$inherited_selection_override" --static-only
  done

  named_source_stage="$temp_root/$name-named-source-stage"
  copy_fixture "$named_source_stage"
  replace_exact_instruction \
    "$named_source_stage/$path" \
    "$source_stage" \
    'FROM golang:1.26.5-alpine AS go-toolchain-source'
  expect_failure_matching \
    "protected source получает подменяемый named-context alias в $name Dockerfile" \
    "$path: Dockerfile stages должны использовать только защищённые BuildKit aliases scratch/context" \
    "$guard" --root "$named_source_stage" --static-only

  named_work_stage="$temp_root/$name-named-work-stage"
  copy_fixture "$named_work_stage"
  replace_exact_instruction \
    "$named_work_stage/$path" \
    "$work_stage" \
    'FROM golang:1.26.5-alpine AS go-tools'
  expect_failure_matching \
    "work stage получает подменяемый named-context alias в $name Dockerfile" \
    "$path: Dockerfile stages должны использовать только защищённые BuildKit aliases scratch/context" \
    "$guard" --root "$named_work_stage" --static-only

  variable_source="$temp_root/$name-variable-source"
  copy_fixture "$variable_source"
  replace_exact_instruction \
    "$variable_source/$path" \
    "$source_stage" \
    'FROM ${GOLANG_IMAGE} AS scratch'
  expect_failure_matching \
    "immutable source использует переменный image в $name Dockerfile" \
    "$path: protected builder stage 0 должна иметь точную защищённую форму 'FROM golang:1.26.5-alpine AS scratch'" \
    "$guard" --root "$variable_source" --static-only

  mutated_immutable_source="$temp_root/$name-mutated-immutable-source"
  copy_fixture "$mutated_immutable_source"
  insert_after_exact_instruction \
    "$mutated_immutable_source/$path" \
    "$source_stage" \
    'RUN rm -rf /usr/local/go'
  expect_failure_matching \
    "write-capable RUN изменяет immutable source stage в $name Dockerfile" \
    "$path: protected builder stage 0 должна первой и единственной инструкцией собирать trusted Go toolchain guard" \
    "$guard" --root "$mutated_immutable_source" --static-only

  mounted_immutable_source="$temp_root/$name-mounted-immutable-source"
  copy_fixture "$mounted_immutable_source"
  insert_after_exact_instruction \
    "$mounted_immutable_source/$path" \
    "$source_stage" \
    'RUN --mount=type=bind,source=.,target=/mnt true'
  expect_failure_matching \
    "RUN mount нарушает immutable source stage в $name Dockerfile" \
    "$path: Dockerfile не должен содержать BuildKit-only RUN mount" \
    "$guard" --root "$mounted_immutable_source" --static-only

  unexpected_stage="$temp_root/$name-unexpected-stage"
  copy_fixture "$unexpected_stage"
  insert_before_exact_instruction \
    "$unexpected_stage/$path" \
    "$final_runtime_from" \
    'FROM attacker.invalid/go:latest'
  expect_failure_matching \
    "внешняя stage сдвигает numeric provenance в $name Dockerfile" \
    "$path: Dockerfile должен содержать ровно три stages" \
    "$guard" --root "$unexpected_stage" --static-only

  fake_go_in_tools_stage="$temp_root/$name-fake-go-in-tools-stage"
  copy_fixture "$fake_go_in_tools_stage"
  insert_before_exact_instruction \
    "$fake_go_in_tools_stage/$path" \
    "$final_runtime_from" \
    "$fake_go_run"
  expect_failure \
    "fake go executable добавлен после trusted guard build в work stage $name Dockerfile" \
    "$guard" --root "$fake_go_in_tools_stage" --static-only

  mutated_guard_source="$temp_root/$name-mutated-guard-source"
  copy_fixture "$mutated_guard_source"
  sed -i '0,/cGFja2FnZSBtYWlu/s//cGFja2FnZSBtYWlo/' "$mutated_guard_source/$path"
  expect_failure_matching \
    "source trusted Go toolchain guard изменён в $name Dockerfile" \
    "$path: protected builder stage 0 должна первой и единственной инструкцией собирать trusted Go toolchain guard" \
    "$guard" --root "$mutated_guard_source" --static-only

  mutated_protected_compiler="$temp_root/$name-mutated-protected-compiler"
  copy_fixture "$mutated_protected_compiler"
  sed -i '0,/\/usr\/local\/go\/bin\/go build/s//go build/' "$mutated_protected_compiler/$path"
  expect_failure_matching \
    "protected builder использует незакреплённый compiler path в $name Dockerfile" \
    "$path: protected builder stage 0 должна первой и единственной инструкцией собирать trusted Go toolchain guard" \
    "$guard" --root "$mutated_protected_compiler" --static-only

  mutated_protected_environment="$temp_root/$name-mutated-protected-environment"
  copy_fixture "$mutated_protected_environment"
  sed -i '0,/GOENV=off/s//GOENV=\/tmp\/attacker-goenv/' "$mutated_protected_environment/$path"
  expect_failure_matching \
    "protected builder использует изменённый build environment в $name Dockerfile" \
    "$path: protected builder stage 0 должна первой и единственной инструкцией собирать trusted Go toolchain guard" \
    "$guard" --root "$mutated_protected_environment" --static-only

  mutated_protected_output="$temp_root/$name-mutated-protected-output"
  copy_fixture "$mutated_protected_output"
  sed -i '0,/-o \/out\/mattercodex-go-toolchain-guard/s//-o \/tmp\/mattercodex-go-toolchain-guard/' "$mutated_protected_output/$path"
  expect_failure_matching \
    "protected builder использует изменённый output path в $name Dockerfile" \
    "$path: protected builder stage 0 должна первой и единственной инструкцией собирать trusted Go toolchain guard" \
    "$guard" --root "$mutated_protected_output" --static-only

  for poison_index in "${!fake_compiler_payloads[@]}"; do
    poisoned_work_compiler="$temp_root/$name-${fake_compiler_names[$poison_index]}-poisoned-work-compiler"
    copy_fixture "$poisoned_work_compiler"
    poison_installer_run \
      "$poisoned_work_compiler/$path" \
      "${fake_compiler_payloads[$poison_index]}"
    expect_failure_matching \
      "${fake_compiler_names[$poison_index]} fake compiler добавлен внутрь существующего installer RUN в $name Dockerfile" \
      "$path work stage source-level contract изменён; install/build instructions и tool outputs не имеют закреплённого provenance" \
      "$guard" --root "$poisoned_work_compiler" --static-only
    expect_failure_matching \
      "full guard отклоняет ${fake_compiler_names[$poison_index]} fake compiler внутри существующего installer RUN в $name Dockerfile" \
      "$path work stage source-level contract изменён; install/build instructions и tool outputs не имеют закреплённого provenance" \
      "$guard" --root "$poisoned_work_compiler"
  done

  external_copy_in_tools_stage="$temp_root/$name-external-copy-in-tools-stage"
  copy_fixture "$external_copy_in_tools_stage"
  insert_before_exact_instruction \
    "$external_copy_in_tools_stage/$path" \
    "$final_runtime_from" \
    'COPY --from=example.invalid/untrusted-go:latest /usr/local/go /usr/local/go'
  expect_failure_matching \
    "external COPY добавлен внутри work stage в $name Dockerfile" \
    "$path: work stage не должна получать данные через COPY --from" \
    "$guard" --root "$external_copy_in_tools_stage" --static-only

  buildkit_mount="$temp_root/$name-buildkit-mount"
  copy_fixture "$buildkit_mount"
  insert_after_exact_instruction \
    "$buildkit_mount/$path" \
    "$bootstrap_copy" \
    'RUN --mount=from=0,source=/usr/local/go,target=/usr/local/go,readonly true'
  expect_failure_matching \
    "BuildKit-only mount возвращён в штатный Kaniko path $name Dockerfile" \
    "$path: Dockerfile не должен содержать BuildKit-only RUN mount: штатная сборка выполняется Kaniko" \
    "$guard" --root "$buildkit_mount" --static-only

  absorbed_go_tools_copy="$temp_root/$name-absorbed-go-tools-copy"
  copy_fixture "$absorbed_go_tools_copy"
  absorb_exact_instruction "$absorbed_go_tools_copy/$path" "$go_tools_copy"
  expect_failure_matching \
    "continuation поглощает обязательный COPY Go tools из numeric stage в $name Dockerfile" \
    "$path: final runtime stage должен дважды копировать закреплённые Go tools" \
    "$guard" --root "$absorbed_go_tools_copy" --static-only

  absorbed_runtime_env="$temp_root/$name-absorbed-runtime-env"
  copy_fixture "$absorbed_runtime_env"
  absorb_exact_instruction \
    "$absorbed_runtime_env/$path" \
    "$runtime_env" \
    'ENV GOTOOLCHAIN="local"'
  expect_failure_matching \
    "continuation поглощает обязательный ENV в $name Dockerfile" \
    "$path: final runtime stage должен содержать точную самостоятельную bootstrap ENV-инструкцию '$runtime_env' ровно один раз" \
    "$guard" --root "$absorbed_runtime_env" --static-only

  absorbed_runtime_copy="$temp_root/$name-absorbed-runtime-copy"
  copy_fixture "$absorbed_runtime_copy"
  absorb_exact_instruction \
    "$absorbed_runtime_copy/$path" \
    "$runtime_copy" \
    'COPY --from=golang:latest /usr/local/go /usr/local/go/'
  expect_failure_matching \
    "continuation поглощает закреплённый COPY и подменяет источник в $name Dockerfile" \
    "$path: final runtime stage содержит неразрешённую COPY-инструкцию с /usr/local/go" \
    "$guard" --root "$absorbed_runtime_copy" --static-only

  escaped_runtime_copy="$temp_root/$name-escaped-runtime-copy"
  copy_fixture "$escaped_runtime_copy"
  printf '%s\n' '' 'COPY --from=golang:latest /usr/local/g\o /usr/local/g\o' >>"$escaped_runtime_copy/$path"
  expect_failure_matching \
    "экранированный внешний COPY подменяет Go toolchain в $name Dockerfile" \
    "$path: final runtime stage содержит write-capable COPY после обязательной stdout verification" \
    "$guard" --root "$escaped_runtime_copy" --static-only

  absorbed_verify="$temp_root/$name-absorbed-verify"
  copy_fixture "$absorbed_verify"
  absorb_exact_instruction "$absorbed_verify/$path" "$verify_run"
  expect_failure_matching \
    "continuation поглощает обязательную stdout verification в $name Dockerfile" \
    "$path: final runtime stage должен выполнять точную shell-independent проверку GOVERSION/GOTOOLCHAIN/GOROOT и compiler probe ровно один раз" \
    "$guard" --root "$absorbed_verify" --static-only

  absorbed_runtime_contract="$temp_root/$name-absorbed-runtime-contract"
  copy_fixture "$absorbed_runtime_contract"
  absorb_exact_instruction \
    "$absorbed_runtime_contract/$path" \
    "$runtime_env" \
    'ENV GOTOOLCHAIN="local"'
  absorb_exact_instruction \
    "$absorbed_runtime_contract/$path" \
    "$runtime_copy" \
    'COPY --from=golang:latest /usr/local/go /usr/local/go/'
  absorb_exact_instruction "$absorbed_runtime_contract/$path" "$verify_run"
  expect_failure_matching \
    "continuation поглощает весь обязательный runtime-контракт в $name Dockerfile" \
    "$path: final runtime stage содержит неразрешённую COPY-инструкцию с /usr/local/go" \
    "$guard" --root "$absorbed_runtime_contract" --static-only

  runtime_env_after_copy="$temp_root/$name-runtime-env-after-copy"
  copy_fixture "$runtime_env_after_copy"
  swap_exact_instructions "$runtime_env_after_copy/$path" "$runtime_env" "$bootstrap_copy"
  expect_failure_matching \
    "логический ENV расположен после bootstrap COPY в $name Dockerfile" \
    "$path: final runtime stage должен задавать GOTOOLCHAIN=local до bootstrap COPY Go toolchain" \
    "$guard" --root "$runtime_env_after_copy" --static-only

  verify_before_copy="$temp_root/$name-verify-before-copy"
  copy_fixture "$verify_before_copy"
  swap_exact_instructions "$verify_before_copy/$path" "$runtime_copy" "$verify_run"
  expect_failure_matching \
    "stdout verification расположена до final trusted COPY в $name Dockerfile" \
    "$path: final runtime stage содержит write-capable COPY после обязательной stdout verification" \
    "$guard" --root "$verify_before_copy" --static-only

  shell_true_before_check="$temp_root/$name-shell-true-before-check"
  copy_fixture "$shell_true_before_check"
  insert_before_exact_instruction \
    "$shell_true_before_check/$path" \
    "$verify_run" \
    'SHELL ["/bin/true"]'
  expect_failure_matching \
    "SHELL /bin/true добавлен перед stdout verification в $name Dockerfile" \
    "$path: final runtime stage не должна содержать SHELL" \
    "$guard" --root "$shell_true_before_check" --static-only

  alternate_shell_before_check="$temp_root/$name-alternate-shell-before-check"
  copy_fixture "$alternate_shell_before_check"
  insert_before_exact_instruction \
    "$alternate_shell_before_check/$path" \
    "$verify_run" \
    'SHELL ["/bin/sh", "-c"]'
  expect_failure_matching \
    "альтернативный SHELL добавлен перед stdout verification в $name Dockerfile" \
    "$path: final runtime stage не должна содержать SHELL" \
    "$guard" --root "$alternate_shell_before_check" --static-only

  replaced_runtime_shell="$temp_root/$name-replaced-runtime-shell"
  copy_fixture "$replaced_runtime_shell"
  insert_before_exact_instruction \
    "$replaced_runtime_shell/$path" \
    "$guard_copy" \
    'RUN rm -f /bin/sh && cp /bin/true /bin/sh'
  expect_failure_matching \
    "дополнительный RUN с подменой /bin/sh отклонён в $name Dockerfile" \
    "$path: final runtime stage содержит" \
    "$guard" --root "$replaced_runtime_shell" --static-only

  destination_only_before_final="$temp_root/$name-destination-only-before-final"
  copy_fixture "$destination_only_before_final"
  insert_before_exact_instruction \
    "$destination_only_before_final/$path" \
    "$guard_copy" \
    "$destination_only_run"
  expect_failure_matching \
    "destination-only GOROOT contamination отклонена до trusted clean в $name Dockerfile" \
    "$path: final runtime stage содержит" \
    "$guard" --root "$destination_only_before_final" --static-only

  fake_go_between_copy_and_check="$temp_root/$name-fake-go-between-copy-and-check"
  copy_fixture "$fake_go_between_copy_and_check"
  insert_before_exact_instruction \
    "$fake_go_between_copy_and_check/$path" \
    "$verify_run" \
    "$fake_go_run"
  expect_failure_matching \
    "fake go executable подменяет toolchain между final COPY и stdout verification в $name Dockerfile" \
    "$path: final runtime stage содержит" \
    "$guard" --root "$fake_go_between_copy_and_check" --static-only

  late_delete_after_check="$temp_root/$name-late-delete-after-check"
  copy_fixture "$late_delete_after_check"
  insert_after_exact_instruction \
    "$late_delete_after_check/$path" \
    "$verify_run" \
    'RUN rm -rf /usr/local/go'
  expect_failure_matching \
    "поздний RUN удаляет Go toolchain после stdout verification в $name Dockerfile" \
    "$path: final runtime stage содержит write-capable RUN после обязательной stdout verification" \
    "$guard" --root "$late_delete_after_check" --static-only

  late_replace_after_check="$temp_root/$name-late-replace-after-check"
  copy_fixture "$late_replace_after_check"
  insert_after_exact_instruction \
    "$late_replace_after_check/$path" \
    "$verify_run" \
    "$fake_go_run"
  expect_failure_matching \
    "поздний RUN заменяет Go executable после stdout verification в $name Dockerfile" \
    "$path: final runtime stage содержит write-capable RUN после обязательной stdout verification" \
    "$guard" --root "$late_replace_after_check" --static-only

  late_external_copy_after_check="$temp_root/$name-late-external-copy-after-check"
  copy_fixture "$late_external_copy_after_check"
  insert_after_exact_instruction \
    "$late_external_copy_after_check/$path" \
    "$verify_run" \
    'COPY --from=example.invalid/untrusted-go:latest /usr/local/go /usr/local/go'
  expect_failure_matching \
    "поздний external COPY заменяет Go toolchain после stdout verification в $name Dockerfile" \
    "$path: final runtime stage содержит write-capable COPY после обязательной stdout verification" \
    "$guard" --root "$late_external_copy_after_check" --static-only

  late_external_add_after_check="$temp_root/$name-late-external-add-after-check"
  copy_fixture "$late_external_add_after_check"
  insert_after_exact_instruction \
    "$late_external_add_after_check/$path" \
    "$verify_run" \
    'ADD https://example.invalid/untrusted-go.tar /usr/local/go/'
  expect_failure_matching \
    "поздний external ADD заменяет Go toolchain после stdout verification в $name Dockerfile" \
    "$path: final runtime stage содержит write-capable ADD после обязательной stdout verification" \
    "$guard" --root "$late_external_add_after_check" --static-only

  unlisted_metadata_after_check="$temp_root/$name-unlisted-metadata-after-check"
  copy_fixture "$unlisted_metadata_after_check"
  insert_after_exact_instruction \
    "$unlisted_metadata_after_check/$path" \
    "$verify_run" \
    'LABEL security.proof=unlisted'
  expect_failure_matching \
    "незаявленная metadata-инструкция добавлена после stdout verification в $name Dockerfile" \
    "$path: после обязательной stdout verification разрешён только точный metadata tail текущего Dockerfile" \
    "$guard" --root "$unlisted_metadata_after_check" --static-only

  duplicate_runtime_env="$temp_root/$name-duplicate-runtime-env"
  copy_fixture "$duplicate_runtime_env"
  printf '\n%s\n' "$runtime_env" >>"$duplicate_runtime_env/$path"
  expect_failure_matching \
    "обязательный логический ENV продублирован в $name Dockerfile" \
    "$path: final runtime stage должен содержать точную самостоятельную bootstrap ENV-инструкцию '$runtime_env' ровно один раз" \
    "$guard" --root "$duplicate_runtime_env" --static-only

  missing_bootstrap_copy="$temp_root/$name-missing-bootstrap-copy"
  copy_fixture "$missing_bootstrap_copy"
  replace_exact_instruction "$missing_bootstrap_copy/$path" "$bootstrap_copy" ""
  expect_failure_matching \
    "Kaniko-compatible bootstrap COPY отсутствует в $name Dockerfile" \
    "$path: final runtime stage должен содержать ровно один Kaniko-compatible bootstrap COPY Go toolchain" \
    "$guard" --root "$missing_bootstrap_copy" --static-only

  missing_trusted_copy="$temp_root/$name-missing-trusted-copy"
  copy_fixture "$missing_trusted_copy"
  replace_exact_instruction "$missing_trusted_copy/$path" "$runtime_copy" ""
  expect_failure_matching \
    "final trusted COPY отсутствует в $name Dockerfile" \
    "$path: final runtime stage должен содержать ровно один final trusted COPY Go toolchain" \
    "$guard" --root "$missing_trusted_copy" --static-only

  replaced_bootstrap_copy="$temp_root/$name-replaced-bootstrap-copy"
  copy_fixture "$replaced_bootstrap_copy"
  replace_exact_instruction \
    "$replaced_bootstrap_copy/$path" \
    "$bootstrap_copy" \
    'COPY --from=1 /usr/local/go/ /opt/mattercodex/bootstrap-go/'
  expect_failure_matching \
    "bootstrap COPY использует не immutable numeric source в $name Dockerfile" \
    "$path: final runtime stage содержит неразрешённую COPY-инструкцию с /usr/local/go" \
    "$guard" --root "$replaced_bootstrap_copy" --static-only

  duplicate_bootstrap_copy="$temp_root/$name-duplicate-bootstrap-copy"
  copy_fixture "$duplicate_bootstrap_copy"
  insert_before_exact_instruction \
    "$duplicate_bootstrap_copy/$path" \
    "$runtime_copy" \
    "$bootstrap_copy"
  expect_failure_matching \
    "Kaniko-compatible bootstrap COPY продублирован в $name Dockerfile" \
    "$path: final runtime stage должен содержать ровно один Kaniko-compatible bootstrap COPY Go toolchain" \
    "$guard" --root "$duplicate_bootstrap_copy" --static-only

  swapped_bootstrap_copy="$temp_root/$name-swapped-bootstrap-copy"
  copy_fixture "$swapped_bootstrap_copy"
  swap_exact_instructions "$swapped_bootstrap_copy/$path" "$bootstrap_copy" "$runtime_copy"
  expect_failure_matching \
    "bootstrap и final trusted COPY переставлены в $name Dockerfile" \
    "$path: Kaniko-compatible bootstrap COPY должен предшествовать trusted COPY Go toolchain guard" \
    "$guard" --root "$swapped_bootstrap_copy" --static-only

  duplicate_runtime_copy="$temp_root/$name-duplicate-runtime-copy"
  copy_fixture "$duplicate_runtime_copy"
  printf '\n%s\n' "$runtime_copy" >>"$duplicate_runtime_copy/$path"
  expect_failure_matching \
    "final trusted COPY продублирован в $name Dockerfile" \
    "$path: final runtime stage должен содержать ровно один final trusted COPY Go toolchain" \
    "$guard" --root "$duplicate_runtime_copy" --static-only

  duplicate_verify="$temp_root/$name-duplicate-verify"
  copy_fixture "$duplicate_verify"
  printf '\n%s\n' "$verify_run" >>"$duplicate_verify/$path"
  expect_failure_matching \
    "stdout verification продублирована в $name Dockerfile" \
    "$path: final runtime stage должен выполнять точную shell-independent проверку GOVERSION/GOTOOLCHAIN/GOROOT и compiler probe ровно один раз" \
    "$guard" --root "$duplicate_verify" --static-only

  missing_guard_copy="$temp_root/$name-missing-guard-copy"
  copy_fixture "$missing_guard_copy"
  replace_exact_instruction "$missing_guard_copy/$path" "$guard_copy" ""
  expect_failure_matching \
    "trusted guard COPY отсутствует в $name Dockerfile" \
    "$path: final runtime stage должен содержать ровно один trusted COPY Go toolchain guard" \
    "$guard" --root "$missing_guard_copy" --static-only

  duplicate_clean="$temp_root/$name-duplicate-clean"
  copy_fixture "$duplicate_clean"
  insert_before_exact_instruction "$duplicate_clean/$path" "$runtime_copy" "$clean_run"
  expect_failure_matching \
    "trusted clean продублирован в $name Dockerfile" \
    "$path: final runtime stage должен выполнять точное trusted очищение GOROOT ровно один раз" \
    "$guard" --root "$duplicate_clean" --static-only

  swapped_clean_copy="$temp_root/$name-swapped-clean-copy"
  copy_fixture "$swapped_clean_copy"
  swap_exact_instructions "$swapped_clean_copy/$path" "$clean_run" "$runtime_copy"
  expect_failure_matching \
    "trusted clean и final COPY переставлены в $name Dockerfile" \
    "$path: final Go tools COPY, trusted guard COPY, clean, final Go COPY, final ENV и verification должны идти непосредственно друг за другом" \
    "$guard" --root "$swapped_clean_copy" --static-only

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
    "$path: final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
    "$guard" --root "$modern_quoted_escape" --static-only

  legacy_quoted_escape="$temp_root/$name-legacy-quoted-escape"
  copy_fixture "$legacy_quoted_escape"
  printf '%s\n' '' 'ENV GOTOOLCHAIN "loc\al"' >>"$legacy_quoted_escape/$path"
  expect_failure_matching \
    "экранирование в кавычках устаревшего ENV в $name Dockerfile" \
    "$path: final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
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

unsafe_local_build_input="$temp_root/unsafe-local-build-input"
copy_fixture "$unsafe_local_build_input"
replace_exact_instruction "$unsafe_local_build_input/scripts/k8s/install-bot-service.sh" "    --frontend-attrs-json '{}'" '    --build-context golang:1.26.5-alpine=docker-image://attacker.invalid/golang:latest'
expect_failure_matching "локальный supported BuildKit entrypoint получает named-context override exact base name" "scripts/k8s/install-bot-service.sh содержит '    --frontend-attrs-json '{}'' 0 раз; ожидается 1" "$guard" --root "$unsafe_local_build_input" --static-only

direct_local_build_input="$temp_root/direct-local-build-input"
copy_fixture "$direct_local_build_input"
printf '\ndocker build -f "$REPO_ROOT/services/jobs/agent-runner/Dockerfile" "$REPO_ROOT"\n' >>"$direct_local_build_input/scripts/k8s/install-bot-service.sh"
expect_failure_matching "локальный supported entrypoint не может обойти защищённый wrapper прямым docker build" "scripts/k8s/install-bot-service.sh содержит 'services/jobs/agent-runner/Dockerfile' 2 раз; ожидается 1" "$guard" --root "$direct_local_build_input" --static-only

unsafe_remote_build_input="$temp_root/unsafe-remote-build-input"
copy_fixture "$unsafe_remote_build_input"
replace_exact_instruction "$unsafe_remote_build_input/scripts/remote/install-bot-service.sh" "        --frontend-attrs-json '{}'\" </dev/null" "        --frontend-attrs-json '{\"context:golang:1.26.5-alpine\":\"docker-image://attacker.invalid/golang:latest\"}'\" </dev/null"
expect_failure_matching "remote supported BuildKit entrypoint получает frontend attrs с override exact base name" "scripts/remote/install-bot-service.sh содержит '        --frontend-attrs-json '{}'\" </dev/null' 0 раз; ожидается 1" "$guard" --root "$unsafe_remote_build_input" --static-only

mutated_build_wrapper="$temp_root/mutated-build-wrapper"
copy_fixture "$mutated_build_wrapper"
printf '\n# unsafe pass-through\n' >>"$mutated_build_wrapper/scripts/build-agent-runner-image.sh"
expect_failure_matching "защищённый BuildKit input wrapper изменён без обновления контракта" "scripts/build-agent-runner-image.sh изменён без обновления защищённого BuildKit input contract" "$guard" --root "$mutated_build_wrapper" --static-only

build_wrapper="$repo_root/scripts/build-agent-runner-image.sh"
fake_builder_bin="$temp_root/fake-builder-bin"
builder_capture="$temp_root/builder-argv"
mkdir -p "$fake_builder_bin"
for builder_name in docker nerdctl; do
  printf '%s\n' '#!/usr/bin/env bash' 'printf '"'"'%s\n'"'"' "$@" >"$MATTERCODEX_BUILDER_CAPTURE"' >"$fake_builder_bin/$builder_name"
  chmod +x "$fake_builder_bin/$builder_name"
done

expect_success "supported Docker entrypoint передаёт canonical agent-runner argv с явно пустыми frontend attrs" env PATH="$fake_builder_bin:$PATH" MATTERCODEX_BUILDER_CAPTURE="$builder_capture" "$build_wrapper" --builder docker --context "$repo_root" --dockerfile "$repo_root/services/jobs/agent-runner/Dockerfile" --tag mattercodex.invalid/agent-runner:test --network host --build-arg MATTERCODEX_CODEX_PACKAGE=@openai/codex@0.144.1 --frontend-attrs-json '{}'
grep -Fqx -- '--network=host' "$builder_capture" || {
  echo "FAIL: canonical Docker wrapper не передал network=host" >&2
  exit 1
}
if grep -Eq -- '(^--build-context|^--opt|context:)' "$builder_capture"; then
  echo "FAIL: canonical Docker wrapper передал named context или frontend attr builder-у" >&2
  exit 1
fi

expect_success "supported nerdctl entrypoint использует неявный пустой frontend attrs allowlist" env PATH="$fake_builder_bin:$PATH" MATTERCODEX_BUILDER_CAPTURE="$builder_capture" "$build_wrapper" --builder nerdctl --context "$repo_root" --dockerfile "$repo_root/deploy/images/agent-runner/Dockerfile" --tag mattercodex.invalid/agent-runner:test
[[ "$(sed -n '1p' "$builder_capture")" == "-n" && "$(sed -n '2p' "$builder_capture")" == "k8s.io" ]] || {
  echo "FAIL: canonical nerdctl wrapper не закрепил namespace k8s.io" >&2
  exit 1
}

for frontend_attrs in '{"context:golang:1.26.5-alpine":"docker-image://attacker.invalid/golang:latest"}' '{"context:scratch":"docker-image://attacker.invalid/scratch:latest"}' '{"context:context":"docker-image://attacker.invalid/context:latest"}' '{"context:docker.io/library/golang:1.26.5-alpine":"docker-image://attacker.invalid/golang:latest"}' '{"context:golang:1.26.5-alpine":"docker-image://attacker.invalid/one","context:scratch":"docker-image://attacker.invalid/two"}' '{"context\u003agolang\u003a1.26.5-alpine":"docker-image://attacker.invalid/golang:latest"}' '{"context:golang:1.26.5-alpine":"docker-image://attacker.invalid/one","context:golang:1.26.5-alpine":"docker-image://attacker.invalid/two"}'; do
  expect_failure_matching "JSON/frontend attrs override защищённого base или alias закрыто отклонён" "allowlist BuildKit frontend attrs для защищённого agent-runner Dockerfile должен быть пустым JSON object" env PATH="$fake_builder_bin:$PATH" MATTERCODEX_BUILDER_CAPTURE="$builder_capture" "$build_wrapper" --builder docker --context "$repo_root" --dockerfile "$repo_root/services/jobs/agent-runner/Dockerfile" --tag mattercodex.invalid/agent-runner:test --frontend-attrs-json "$frontend_attrs"
done

expect_failure_matching "exact Docker --build-context mapping защищённого base закрыто отклонён" "named contexts и произвольные BuildKit frontend attrs запрещены" "$build_wrapper" --build-context golang:1.26.5-alpine=docker-image://attacker.invalid/golang:latest
expect_failure_matching "encoded --build-context mapping защищённого base закрыто отклонён" "named contexts и произвольные BuildKit frontend attrs запрещены" "$build_wrapper" '--build-context=golang%3A1.26.5-alpine=docker-image%3A%2F%2Fattacker.invalid%2Fgolang%3Alatest'
expect_failure_matching "raw buildctl --opt context mapping закрыто отклонён" "named contexts и произвольные BuildKit frontend attrs запрещены" "$build_wrapper" --opt=context:golang:1.26.5-alpine=docker-image://attacker.invalid/golang:latest
expect_failure_matching "duplicate frontend attrs input закрыто отклонён" "--frontend-attrs-json нельзя передавать повторно" "$build_wrapper" --frontend-attrs-json '{}' --frontend-attrs-json '{}'
expect_failure_matching "BUILDKIT_SYNTAX build arg не может выбрать внешний frontend" "для защищённого agent-runner Dockerfile разрешён только build arg MATTERCODEX_CODEX_PACKAGE" "$build_wrapper" --builder docker --context "$repo_root" --dockerfile "$repo_root/services/jobs/agent-runner/Dockerfile" --tag mattercodex.invalid/agent-runner:test --build-arg BUILDKIT_SYNTAX=attacker.invalid/frontend:latest
expect_failure_matching "build context не может превратиться в builder option" "build context не должен интерпретироваться как builder option" "$build_wrapper" --builder docker --context '--build-context=golang:1.26.5-alpine=docker-image://attacker.invalid/golang:latest' --dockerfile services/jobs/agent-runner/Dockerfile --tag mattercodex.invalid/agent-runner:test

buildkit_probe_root="$temp_root/buildkit-probe"
mkdir -p "$buildkit_probe_root"
cat >"$buildkit_probe_root/go.mod" <<'EOF'
module mattercodex.invalid/buildkit-probe

go 1.26.0

require github.com/moby/buildkit v0.29.0
EOF
cat >"$buildkit_probe_root/main.go" <<'EOF'
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/sourceresolver"
	"github.com/moby/buildkit/frontend/dockerfile/dockerfile2llb"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/moby/buildkit/frontend/dockerfile/shell"
	"github.com/moby/buildkit/frontend/dockerui"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	gatewaypb "github.com/moby/buildkit/frontend/gateway/pb"
	"github.com/moby/buildkit/solver/pb"
	digest "github.com/opencontainers/go-digest"
	fstypes "github.com/tonistiigi/fsutil/types"
)

var errAttackerImageRequested = errors.New("запрошен attacker image через named context")

const imageConfig = `{"architecture":"amd64","os":"linux","config":{"Env":["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"]},"rootfs":{"type":"layers","diff_ids":[]}}`

type gatewayProbe struct {
	opts     gateway.BuildOpts
	requests []string
}

type probeReference struct{}

func (*probeReference) ToState() (llb.State, error) {
	return llb.Scratch(), nil
}

func (*probeReference) Evaluate(context.Context) error {
	return nil
}

func (*probeReference) ReadFile(context.Context, gateway.ReadRequest) ([]byte, error) {
	return nil, os.ErrNotExist
}

func (*probeReference) StatFile(context.Context, gateway.StatRequest) (*fstypes.Stat, error) {
	return nil, os.ErrNotExist
}

func (*probeReference) ReadDir(context.Context, gateway.ReadDirRequest) ([]*fstypes.Stat, error) {
	return nil, os.ErrNotExist
}

func (p *gatewayProbe) ResolveSourceMetadata(context.Context, *pb.SourceOp, sourceresolver.Opt) (*sourceresolver.MetaResponse, error) {
	return nil, errors.New("неожиданный ResolveSourceMetadata")
}

func (p *gatewayProbe) ResolveImageConfig(_ context.Context, ref string, _ sourceresolver.Opt) (string, digest.Digest, []byte, error) {
	p.requests = append(p.requests, ref)
	if strings.Contains(ref, "attacker.invalid") {
		return "", "", nil, errAttackerImageRequested
	}
	return ref, digest.FromString(ref), []byte(imageConfig), nil
}

func (p *gatewayProbe) Solve(context.Context, gateway.SolveRequest) (*gateway.Result, error) {
	result := gateway.NewResult()
	result.SetRef(&probeReference{})
	return result, nil
}

func (p *gatewayProbe) BuildOpts() gateway.BuildOpts {
	return p.opts
}

func (p *gatewayProbe) Inputs(context.Context) (map[string]llb.State, error) {
	return map[string]llb.State{}, nil
}

func (p *gatewayProbe) NewContainer(context.Context, gateway.NewContainerRequest) (gateway.Container, error) {
	return nil, errors.New("неожиданный NewContainer")
}

func (p *gatewayProbe) Warn(context.Context, digest.Digest, string, gateway.WarnOpts) error {
	return nil
}

type imageResolver struct{}

func (imageResolver) ResolveImageConfig(_ context.Context, ref string, _ sourceresolver.Opt) (string, digest.Digest, []byte, error) {
	return ref, digest.FromString(ref), []byte(imageConfig), nil
}

func parseContract(path string, data []byte) error {
	parsed, err := parser.Parse(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parser.Parse %s: %w", path, err)
	}
	stages, _, err := instructions.Parse(parsed.AST, nil)
	if err != nil {
		return fmt.Errorf("instructions.Parse %s: %w", path, err)
	}
	if len(stages) != 3 {
		return fmt.Errorf("%s: найдено %d stages вместо 3", path, len(stages))
	}
	if stages[0].Name != "scratch" || stages[1].Name != "context" || stages[2].Name != "" {
		return fmt.Errorf("%s: aliases=%q,%q,%q вместо защищённых scratch/context", path, stages[0].Name, stages[1].Name, stages[2].Name)
	}
	if stages[0].BaseName != "golang:1.26.5-alpine" || len(stages[0].Commands) != 1 {
		return fmt.Errorf("%s: stage 0 не является exact protected builder", path)
	}
	protectedBuild, ok := stages[0].Commands[0].(*instructions.RunCommand)
	if !ok || !protectedBuild.PrependShell || !strings.Contains(strings.Join(protectedBuild.CmdLine, " "), "/usr/local/go/bin/go build -trimpath -buildvcs=false") {
		return fmt.Errorf("%s: stage 0 не содержит exact protected compiler path/argv", path)
	}

	lex := shell.NewLex(parsed.EscapeToken)
	finalStage := stages[len(stages)-1]
	var sourceCopies, workCopies, cleanChecks, outputChecks int
	var bootstrapCopyIndex, guardCopyIndex, cleanIndex, runtimeCopyIndex, finalEnvIndex, verifyIndex int
	var finalToolsCopyIndices []int
	finalEnvValue := ""
	for _, stage := range stages {
		for _, command := range stage.Commands {
			switch typed := command.(type) {
			case *instructions.CopyCommand:
				if typed.From == "" {
					continue
				}
				processed, err := lex.ProcessWordWithMatches(typed.From, shell.EnvsFromSlice(nil))
				if err != nil || processed.Result != typed.From {
					return fmt.Errorf("%s: shell resolver изменил COPY --from=%q", path, typed.From)
				}
				if _, err := strconv.Atoi(typed.From); err != nil {
					return fmt.Errorf("%s: COPY --from=%q не является internal numeric stage", path, typed.From)
				}
			}
			if run, ok := command.(*instructions.RunCommand); ok && len(instructions.GetMounts(run)) != 0 {
				return fmt.Errorf("%s: BuildKit-only RUN mount нарушает Kaniko contract", path)
			}
		}
	}
	for commandIndex, command := range finalStage.Commands {
		switch typed := command.(type) {
		case *instructions.CopyCommand:
			switch typed.From {
			case "0":
				sourceCopies++
			case "1":
				workCopies++
			}
			if typed.From == "1" && len(typed.SourcePaths) == 1 && typed.SourcePaths[0] == "/tool-bin/" && typed.DestPath == "/usr/local/bin/" {
				finalToolsCopyIndices = append(finalToolsCopyIndices, commandIndex)
			}
			if typed.From == "0" && len(typed.SourcePaths) == 1 && typed.SourcePaths[0] == "/usr/local/go/" && typed.DestPath == "/opt/mattercodex/bootstrap-go/" {
				bootstrapCopyIndex = commandIndex
			}
			if typed.From == "0" && len(typed.SourcePaths) == 1 && typed.SourcePaths[0] == "/out/mattercodex-go-toolchain-guard" && typed.DestPath == "/usr/local/libexec/mattercodex-go-toolchain-guard" {
				guardCopyIndex = commandIndex
			}
			if typed.From == "0" && len(typed.SourcePaths) == 1 && typed.SourcePaths[0] == "/usr/local/go/" && typed.DestPath == "/usr/local/go/" {
				runtimeCopyIndex = commandIndex
			}
		case *instructions.RunCommand:
			if typed.PrependShell {
				continue
			}
			joined := strings.Join(typed.CmdLine, "\x00")
			switch joined {
			case "/usr/local/libexec/mattercodex-go-toolchain-guard\x00clean":
				cleanChecks++
				cleanIndex = commandIndex
			case "/usr/local/libexec/mattercodex-go-toolchain-guard\x00verify\x00/usr/local/go/bin/go":
				outputChecks++
				verifyIndex = commandIndex
			}
		case *instructions.EnvCommand:
			parts := make([]string, 0, len(typed.Env))
			for _, pair := range typed.Env {
				parts = append(parts, pair.String())
			}
			candidate := strings.Join(parts, " ")
			if strings.HasPrefix(candidate, "GOROOT=/usr/local/go GOENV=off GOFLAGS= GOTOOLCHAIN=local PATH=") {
				finalEnvValue = candidate
				finalEnvIndex = commandIndex
			}
		}
	}
	expectedWorkCopies := 2
	if strings.Contains(path, "services/jobs") {
		expectedWorkCopies = 3
	}
	if sourceCopies != 3 || workCopies != expectedWorkCopies || cleanChecks != 1 || outputChecks != 1 {
		return fmt.Errorf("%s: parser contract source=%d work=%d clean=%d verify=%d", path, sourceCopies, workCopies, cleanChecks, outputChecks)
	}
	expectedFinalEnv := "GOROOT=/usr/local/go GOENV=off GOFLAGS= GOTOOLCHAIN=local PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin"
	if strings.Contains(path, "services/jobs") {
		expectedFinalEnv += " PLAYWRIGHT_BROWSERS_PATH=/ms-playwright"
	}
	if len(finalToolsCopyIndices) != 2 || finalEnvValue != expectedFinalEnv {
		return fmt.Errorf("%s: parser не подтвердил два exact Go tools COPY и exact final environment", path)
	}
	if finalToolsCopyIndices[0] >= bootstrapCopyIndex || finalToolsCopyIndices[1]+1 != guardCopyIndex || guardCopyIndex+1 != cleanIndex || cleanIndex+1 != runtimeCopyIndex || runtimeCopyIndex+1 != finalEnvIndex || finalEnvIndex+1 != verifyIndex {
		return fmt.Errorf("%s: parser не подтвердил protected final source/order tail", path)
	}
	return nil
}

func convertWithContexts(data []byte, attrs map[string]string) ([]string, error) {
	caps := pb.Caps.CapSet(pb.Caps.All())
	probe := &gatewayProbe{opts: gateway.BuildOpts{
		Opts:    attrs,
		LLBCaps: caps,
		Caps:    gatewaypb.Caps.CapSet(gatewaypb.Caps.All()),
	}}
	client, err := dockerui.NewClient(probe)
	if err != nil {
		return nil, err
	}
	_, err = dockerfile2llb.Dockerfile2LLB(context.Background(), data, dockerfile2llb.ConvertOpt{
		Client:       client,
		MetaResolver: imageResolver{},
		LLBCaps:      &caps,
		AllStages:    true,
	})
	return probe.requests, err
}

func replaceProtectedAlias(data []byte, occurrence int, alias string) ([]byte, error) {
	lines := strings.Split(string(data), "\n")
	selected := 0
	for index, line := range lines {
		if line != "FROM golang:1.26.5-alpine AS scratch" && line != "FROM golang:1.26.5-alpine AS context" {
			continue
		}
		selected++
		if selected == occurrence {
			lines[index] = "FROM golang:1.26.5-alpine AS " + alias
			return []byte(strings.Join(lines, "\n")), nil
		}
	}
	return nil, fmt.Errorf("не найдена FROM occurrence %d", occurrence)
}

func verifyPath(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := parseContract(path, data); err != nil {
		return err
	}
	requests, err := convertWithContexts(data, map[string]string{})
	if err != nil {
		return fmt.Errorf("canonical empty-attrs Dockerfile2LLB %s: %w", path, err)
	}
	if len(requests) != 0 {
		return fmt.Errorf("%s: canonical protected stages вызвали external image requests: %v", path, requests)
	}
	for _, mutation := range []struct {
		occurrence int
		alias      string
	}{{1, "go-toolchain-source"}, {2, "go-tools"}} {
		mutated, err := replaceProtectedAlias(data, mutation.occurrence, mutation.alias)
		if err != nil {
			return err
		}
		requests, err := convertWithContexts(mutated, map[string]string{
			"context:scratch":             "docker-image://attacker.invalid/scratch:latest",
			"context:context":             "docker-image://attacker.invalid/context:latest",
			"context:go-toolchain-source": "docker-image://attacker.invalid/go:latest",
			"context:go-tools":            "docker-image://attacker.invalid/tools:latest",
		})
		if !errors.Is(err, errAttackerImageRequested) || len(requests) != 1 || !strings.Contains(requests[0], "attacker.invalid") {
			return fmt.Errorf("%s: ConvertOpt.Client не доказал substitution alias %s: requests=%v err=%v", path, mutation.alias, requests, err)
		}
	}
	requests, err = convertWithContexts(data, map[string]string{
		"context:golang:1.26.5-alpine": "docker-image://attacker.invalid/golang:latest",
	})
	if !errors.Is(err, errAttackerImageRequested) || len(requests) < 1 {
		return fmt.Errorf("%s: ConvertOpt.Client не воспроизвёл substitution exact base name: requests=%v err=%v", path, requests, err)
	}
	for _, request := range requests {
		if !strings.Contains(request, "attacker.invalid") {
			return fmt.Errorf("%s: exact base substitution содержит неожиданный request=%q", path, request)
		}
	}
	return nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "нужны пути к двум Dockerfile")
		os.Exit(2)
	}
	for _, path := range os.Args[1:] {
		if err := verifyPath(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
EOF
expect_success \
  "official BuildKit v0.29.0 parser/instructions/shell и Dockerfile2LLB ConvertOpt.Client contract" \
  env GOENV=off GOWORK=off go -C "$buildkit_probe_root" run -mod=mod . \
    "$repo_root/services/jobs/agent-runner/Dockerfile" \
    "$repo_root/deploy/images/agent-runner/Dockerfile"

kaniko_probe_root="$temp_root/kaniko-probe"
mkdir -p "$kaniko_probe_root"
cat >"$kaniko_probe_root/go.mod" <<'EOF'
module mattercodex.invalid/kaniko-probe

go 1.26.0

require github.com/GoogleContainerTools/kaniko v1.24.0
EOF
cat >"$kaniko_probe_root/main.go" <<'EOF'
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/GoogleContainerTools/kaniko/pkg/config"
	kanikodockerfile "github.com/GoogleContainerTools/kaniko/pkg/dockerfile"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
)

func verify(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	stages, metaArgs, err := kanikodockerfile.Parse(data)
	if err != nil {
		return fmt.Errorf("Parse %s: %w", path, err)
	}
	kanikoStages, err := kanikodockerfile.MakeKanikoStages(&config.KanikoOptions{SkipUnusedStages: true}, stages, metaArgs)
	if err != nil {
		return fmt.Errorf("MakeKanikoStages %s: %w", path, err)
	}
	if len(kanikoStages) != 3 {
		return fmt.Errorf("%s: SkipUnusedStages=true сохранил %d stages вместо 3", path, len(kanikoStages))
	}
	if kanikoStages[0].Name != "scratch" || kanikoStages[1].Name != "context" || kanikoStages[2].Name != "" {
		return fmt.Errorf("%s: неожиданные Kaniko stage names %q,%q,%q", path, kanikoStages[0].Name, kanikoStages[1].Name, kanikoStages[2].Name)
	}
	var sourceCopies, workCopies int
	for stageIndex, stage := range kanikoStages {
		for _, command := range stage.Commands {
			copyCommand, ok := command.(*instructions.CopyCommand)
			if !ok || copyCommand.From == "" {
				continue
			}
			fromIndex, err := strconv.Atoi(copyCommand.From)
			if err != nil || fromIndex < 0 || fromIndex >= stageIndex {
				return fmt.Errorf("%s: COPY --from=%q не разрешается в сохранённую предыдущую stage", path, copyCommand.From)
			}
			switch fromIndex {
			case 0:
				sourceCopies++
			case 1:
				workCopies++
			}
		}
	}
	expectedWorkCopies := 2
	if strings.Contains(path, "services/jobs") {
		expectedWorkCopies = 3
	}
	if sourceCopies != 3 || workCopies != expectedWorkCopies {
		return fmt.Errorf("%s: Kaniko dependencies source=%d work=%d", path, sourceCopies, workCopies)
	}
	return nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "нужны пути к двум Dockerfile")
		os.Exit(2)
	}
	for _, path := range os.Args[1:] {
		if err := verify(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
EOF
expect_success \
  "exact Kaniko v1.24.0 Parse + MakeKanikoStages с SkipUnusedStages=true" \
  env GOENV=off GOWORK=off go -C "$kaniko_probe_root" run -mod=mod . \
    "$repo_root/services/jobs/agent-runner/Dockerfile" \
    "$repo_root/deploy/images/agent-runner/Dockerfile"
kaniko_module_json="$(env GOENV=off GOWORK=off go -C "$kaniko_probe_root" mod download -json github.com/GoogleContainerTools/kaniko@v1.24.0)"
kaniko_origin_hash="$(sed -n 's/^[[:space:]]*"Hash": "\([0-9a-f]*\)",$/\1/p' <<<"$kaniko_module_json")"
[[ "$kaniko_origin_hash" == "1d2bff595903b1887220a522a5a43c67db2da553" ]] || {
  echo "FAIL: Kaniko v1.24.0 имеет неожиданный upstream commit" >&2
  exit 1
}

trusted_guard_services_base64="$(sed -n "s/^[[:space:]]*&& printf '%s' '\([^']*\)'.*/\1/p" "$repo_root/services/jobs/agent-runner/Dockerfile")"
trusted_guard_deploy_base64="$(sed -n "s/^[[:space:]]*&& printf '%s' '\([^']*\)'.*/\1/p" "$repo_root/deploy/images/agent-runner/Dockerfile")"
[[ -n "$trusted_guard_services_base64" && "$trusted_guard_services_base64" == "$trusted_guard_deploy_base64" ]] || {
  echo "FAIL: обе Dockerfile должны собирать trusted guard из одного закреплённого source" >&2
  exit 1
}
trusted_guard_source="$temp_root/mattercodex-go-toolchain-guard.go"
trusted_guard_binary="$temp_root/mattercodex-go-toolchain-guard"
printf '%s' "$trusted_guard_services_base64" | base64 -d >"$trusted_guard_source"
expect_success \
  "trusted Go toolchain guard собирается из фактического Dockerfile source" \
  env CGO_ENABLED=0 GOENV=off GOTOOLCHAIN=local GOWORK=off go build -trimpath -buildvcs=false -o "$trusted_guard_binary" "$trusted_guard_source"
expect_success \
  "trusted Go toolchain guard является Go artifact, а не no-op compiler output" \
  env GOENV=off GOTOOLCHAIN=local GOWORK=off go version -m "$trusted_guard_binary"

cleanup_go_root="$temp_root/cleanup-root/usr/local/go"
cleanup_bootstrap_root="$temp_root/cleanup-root/opt/mattercodex/bootstrap-go"
cleanup_guard_source="$temp_root/mattercodex-go-toolchain-cleanup-guard.go"
cleanup_guard_binary="$temp_root/mattercodex-go-toolchain-cleanup-guard"
sed \
  -e "s#/usr/local/go#$cleanup_go_root#g" \
  -e "s#/opt/mattercodex/bootstrap-go#$cleanup_bootstrap_root#g" \
  "$trusted_guard_source" >"$cleanup_guard_source"
expect_success \
  "trusted cleaner собирается с изолированными behavioral destinations" \
  env CGO_ENABLED=0 GOENV=off GOTOOLCHAIN=local GOWORK=off go build -trimpath -buildvcs=false -o "$cleanup_guard_binary" "$cleanup_guard_source"
mkdir -p "$cleanup_go_root/src/runtime" "$cleanup_bootstrap_root/src/runtime"
printf '%s\n' contaminated >"$cleanup_go_root/src/runtime/mattercodex-injected.go"
printf '%s\n' contaminated >"$cleanup_bootstrap_root/src/runtime/mattercodex-injected.go"
expect_success \
  "trusted cleaner удаляет destination-only GOROOT и bootstrap contamination" \
  "$cleanup_guard_binary" clean
[[ ! -e "$cleanup_go_root" && ! -e "$cleanup_bootstrap_root" ]] || {
  echo "FAIL: trusted cleaner сохранил destination-only contamination" >&2
  exit 1
}

correct_go="$temp_root/correct-go"
cat >"$correct_go" <<'EOF'
#!/usr/bin/env bash
[[ "${GOROOT:-}" == /usr/local/go && "${GOENV:-}" == off && "${GOTOOLCHAIN:-}" == local ]]
[[ "${GOFLAGS+x}" == x && -z "${GOFLAGS:-}" && "${GOWORK:-}" == off ]]
[[ -z "${MATTERCODEX_ATTACKER_MARKER:-}" ]]
case "$*" in
  "env GOVERSION") printf 'go1.26.5\n' ;;
  "env GOTOOLCHAIN") printf 'local\n' ;;
  "env GOROOT") printf '/usr/local/go\n' ;;
  "tool compile -V=full") printf 'compile version go1.26.5\n' ;;
  *) exit 2 ;;
esac
EOF
wrong_version_go="$temp_root/wrong-version-go"
cat >"$wrong_version_go" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  "env GOVERSION") printf 'go1.26.4\n' ;;
  "env GOTOOLCHAIN") printf 'local\n' ;;
  "env GOROOT") printf '/usr/local/go\n' ;;
  "tool compile -V=full") printf 'compile version go1.26.5\n' ;;
  *) exit 2 ;;
esac
EOF
wrong_toolchain_go="$temp_root/wrong-toolchain-go"
cat >"$wrong_toolchain_go" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  "env GOVERSION") printf 'go1.26.5\n' ;;
  "env GOTOOLCHAIN") printf 'auto\n' ;;
  "env GOROOT") printf '/usr/local/go\n' ;;
  "tool compile -V=full") printf 'compile version go1.26.5\n' ;;
  *) exit 2 ;;
esac
EOF
wrong_goroot_go="$temp_root/wrong-goroot-go"
cat >"$wrong_goroot_go" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  "env GOVERSION") printf 'go1.26.5\n' ;;
  "env GOTOOLCHAIN") printf 'local\n' ;;
  "env GOROOT") printf '/opt/mattercodex/attacker-go\n' ;;
  "tool compile -V=full") printf 'compile version go1.26.5\n' ;;
  *) exit 2 ;;
esac
EOF
wrong_compile_go="$temp_root/wrong-compile-go"
cat >"$wrong_compile_go" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  "env GOVERSION") printf 'go1.26.5\n' ;;
  "env GOTOOLCHAIN") printf 'local\n' ;;
  "env GOROOT") printf '/usr/local/go\n' ;;
  "tool compile -V=full") printf 'compile version attacker\n' ;;
  *) exit 2 ;;
esac
EOF
missing_compile_go="$temp_root/missing-compile-go"
cat >"$missing_compile_go" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  "env GOVERSION") printf 'go1.26.5\n' ;;
  "env GOTOOLCHAIN") printf 'local\n' ;;
  "env GOROOT") printf '/usr/local/go\n' ;;
  "tool compile -V=full") exit 2 ;;
  *) exit 2 ;;
esac
EOF
chmod +x "$correct_go" "$wrong_version_go" "$wrong_toolchain_go" "$wrong_goroot_go" "$wrong_compile_go" "$missing_compile_go"
expect_success \
  "trusted guard очищает inherited environment и принимает точные GOVERSION/GOTOOLCHAIN/GOROOT/compiler stdout" \
  env GOROOT=/opt/mattercodex/attacker-go GOENV=/tmp/attacker-goenv GOTOOLCHAIN=auto MATTERCODEX_ATTACKER_MARKER=present "$trusted_guard_binary" verify "$correct_go"
expect_failure_matching \
  "exit 0 с неверным GOVERSION stdout отклонён trusted guard" \
  "GOVERSION mismatch" \
  "$trusted_guard_binary" verify "$wrong_version_go"
expect_failure_matching \
  "exit 0 с неверным GOTOOLCHAIN stdout отклонён trusted guard" \
  "GOTOOLCHAIN mismatch" \
  "$trusted_guard_binary" verify "$wrong_toolchain_go"
expect_failure_matching "exit 0 с неверным GOROOT stdout отклонён trusted guard" "GOROOT mismatch" "$trusted_guard_binary" verify "$wrong_goroot_go"
expect_failure_matching "exit 0 с неверным compiler probe stdout отклонён trusted guard" "go tool compile mismatch" "$trusted_guard_binary" verify "$wrong_compile_go"
expect_failure_matching "неполный GOROOT без compiler tool отклонён trusted guard" "go tool compile failed" "$trusted_guard_binary" verify "$missing_compile_go"
mkdir -p "$temp_root/attacker-empty-goroot"
expect_success "trusted guard игнорирует inherited GOROOT при проверке фактического Go toolchain" env GOROOT="$temp_root/attacker-empty-goroot" GOENV="$temp_root/attacker-goenv" GOTOOLCHAIN=auto "$trusted_guard_binary" verify /usr/local/go/bin/go

logical_contract_continuations="$temp_root/logical-contract-continuations"
copy_fixture "$logical_contract_continuations"
for path in "${agent_runner_paths[@]}"; do
  split_exact_instruction \
    "$logical_contract_continuations/$path" \
    "$runtime_copy" \
    'COPY --from=0 /usr/local/go/ \' \
    '/usr/local/go/'
  split_exact_instruction \
    "$logical_contract_continuations/$path" \
    "$verify_run" \
    'RUN ["/usr/local/libexec/mattercodex-go-toolchain-guard", "verify", \' \
    '"/usr/local/go/bin/go"]'
done
expect_success \
  "самостоятельные обязательные COPY и trusted verify поддерживают ordinary continuation" \
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
  insert_after_nth_exact_instruction \
    "$ascii_indentation_local/$path" \
    "$go_tools_copy" \
    1 \
    $' \tENV GOTOOLCHAIN="local"'
done
expect_failure_matching \
  "ASCII-пробел и табуляция не скрывают дополнительный GOTOOLCHAIN ENV" \
  "final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
  "$guard" --root "$ascii_indentation_local" --static-only

ascii_continuation_suffix="$temp_root/ascii-continuation-suffix"
copy_fixture "$ascii_continuation_suffix"
for path in "${agent_runner_paths[@]}"; do
  insert_after_nth_exact_instruction \
    "$ascii_continuation_suffix/$path" \
    "$go_tools_copy" \
    1 \
    $'ENV PATH=/usr/local/go/bin\\ \t\n    GOTOOLCHAIN="local"'
done
expect_failure_matching \
  "ASCII-пробел и табуляция после escape не скрывают дополнительный GOTOOLCHAIN ENV" \
  "final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
  "$guard" --root "$ascii_continuation_suffix" --static-only

modern_quoted_local="$temp_root/modern-quoted-local"
copy_fixture "$modern_quoted_local"
for path in "${agent_runner_paths[@]}"; do
  insert_after_nth_exact_instruction \
    "$modern_quoted_local/$path" \
    "$go_tools_copy" \
    1 \
    'ENV GOTOOLCHAIN="local"'
done
expect_failure_matching \
  "современный quoted ENV не создаёт разрешённый дубликат GOTOOLCHAIN" \
  "final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
  "$guard" --root "$modern_quoted_local" --static-only

legacy_quoted_local="$temp_root/legacy-quoted-local"
copy_fixture "$legacy_quoted_local"
for path in "${agent_runner_paths[@]}"; do
  insert_after_nth_exact_instruction \
    "$legacy_quoted_local/$path" \
    "$go_tools_copy" \
    1 \
    'ENV GOTOOLCHAIN "local"'
done
expect_failure_matching \
  "legacy quoted ENV не создаёт разрешённый дубликат GOTOOLCHAIN" \
  "final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
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
sed -i '/^RUN \["\/usr\/local\/libexec\/mattercodex-go-toolchain-guard", "verify", "\/usr\/local\/go\/bin\/go"\]$/d' "$runtime_check_missing/deploy/images/agent-runner/Dockerfile"
expect_failure "точная stdout verification отсутствует в deploy final runtime stage" "$guard" --root "$runtime_check_missing" --static-only

runtime_final_local="$temp_root/runtime-final-local"
copy_fixture "$runtime_final_local"
for path in "${agent_runner_paths[@]}"; do
  insert_after_nth_exact_instruction \
    "$runtime_final_local/$path" \
    "$go_tools_copy" \
    1 \
    $'ENV GOTOOLCHAIN=auto\nENV PATH=/usr/local/go/bin:/usr/local/bin \\\n    GOTOOLCHAIN="local"'
done
expect_failure_matching "повторные GOTOOLCHAIN ENV запрещены даже при final local" "final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" "$guard" --root "$runtime_final_local" --static-only

legacy_runtime_final_local="$temp_root/legacy-runtime-final-local"
copy_fixture "$legacy_runtime_final_local"
insert_after_nth_exact_instruction \
  "$legacy_runtime_final_local/services/jobs/agent-runner/Dockerfile" \
  "$go_tools_copy" \
  1 \
  $'ENV GOTOOLCHAIN auto\nENV GOTOOLCHAIN local'
expect_failure_matching "повторный legacy GOTOOLCHAIN ENV запрещён даже при final local" "final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" "$guard" --root "$legacy_runtime_final_local" --static-only

services_runtime_override="$temp_root/services-runtime-override"
copy_fixture "$services_runtime_override"
printf '\nENV GOTOOLCHAIN=auto\n' >>"$services_runtime_override/services/jobs/agent-runner/Dockerfile"
expect_failure_matching \
  "поздний GOTOOLCHAIN=auto в services final runtime stage" \
  "services/jobs/agent-runner/Dockerfile: final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
  "$guard" --root "$services_runtime_override" --static-only

deploy_runtime_override="$temp_root/deploy-runtime-override"
copy_fixture "$deploy_runtime_override"
printf '\nENV GOTOOLCHAIN=auto\n' >>"$deploy_runtime_override/deploy/images/agent-runner/Dockerfile"
expect_failure_matching \
  "поздний GOTOOLCHAIN=auto в deploy final runtime stage" \
  "deploy/images/agent-runner/Dockerfile: final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
  "$guard" --root "$deploy_runtime_override" --static-only

services_multiline_override="$temp_root/services-multiline-override"
copy_fixture "$services_multiline_override"
cat >>"$services_multiline_override/services/jobs/agent-runner/Dockerfile" <<'EOF'

ENV PATH=/usr/local/go/bin:/usr/local/bin \
    GOTOOLCHAIN=auto
EOF
expect_failure_matching \
  "многострочный GOTOOLCHAIN=auto в services final runtime stage" \
  "services/jobs/agent-runner/Dockerfile: final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
  "$guard" --root "$services_multiline_override" --static-only

deploy_multiline_override="$temp_root/deploy-multiline-override"
copy_fixture "$deploy_multiline_override"
cat >>"$deploy_multiline_override/deploy/images/agent-runner/Dockerfile" <<'EOF'

ENV PATH=/usr/local/go/bin:/usr/local/bin \
    GOTOOLCHAIN=auto
EOF
expect_failure_matching \
  "многострочный GOTOOLCHAIN=auto в deploy final runtime stage" \
  "deploy/images/agent-runner/Dockerfile: final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
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
  "services/jobs/agent-runner/Dockerfile: Dockerfile должен содержать ровно три stages" \
  "$guard" --root "$services_lowercase_final_from" --static-only

deploy_indented_final_from="$temp_root/deploy-indented-final-from"
copy_fixture "$deploy_indented_final_from"
printf '\n  FROM scratch\n' >>"$deploy_indented_final_from/deploy/images/agent-runner/Dockerfile"
expect_failure_matching \
  "новая final stage с отступом в deploy Dockerfile" \
  "deploy/images/agent-runner/Dockerfile: Dockerfile должен содержать ровно три stages" \
  "$guard" --root "$deploy_indented_final_from" --static-only

services_three_slash_final_from="$temp_root/services-three-slash-final-from"
copy_fixture "$services_three_slash_final_from"
cat >>"$services_three_slash_final_from/services/jobs/agent-runner/Dockerfile" <<'EOF'

RUN true \\\
FROM scratch
EOF
expect_failure_matching \
  "три завершающих backslash перед новой final stage в services Dockerfile" \
  "services/jobs/agent-runner/Dockerfile: Dockerfile должен содержать ровно три stages" \
  "$guard" --root "$services_three_slash_final_from" --static-only

deploy_three_slash_final_from="$temp_root/deploy-three-slash-final-from"
copy_fixture "$deploy_three_slash_final_from"
cat >>"$deploy_three_slash_final_from/deploy/images/agent-runner/Dockerfile" <<'EOF'

RUN true \\\
FROM scratch
EOF
expect_failure_matching \
  "три завершающих backslash перед новой final stage в deploy Dockerfile" \
  "deploy/images/agent-runner/Dockerfile: Dockerfile должен содержать ровно три stages" \
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
  "services/jobs/agent-runner/Dockerfile: final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
  "$guard" --root "$services_split_instruction" --static-only

services_split_key="$temp_root/services-split-key"
copy_fixture "$services_split_key"
cat >>"$services_split_key/services/jobs/agent-runner/Dockerfile" <<'EOF'

ENV GOTOO\
LCHAIN=auto
EOF
expect_failure_matching \
  "разрыв имени GOTOOLCHAIN в services Dockerfile" \
  "services/jobs/agent-runner/Dockerfile: final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
  "$guard" --root "$services_split_key" --static-only

deploy_split_instruction="$temp_root/deploy-split-instruction"
copy_fixture "$deploy_split_instruction"
cat >>"$deploy_split_instruction/deploy/images/agent-runner/Dockerfile" <<'EOF'

EN\
V GOTOOLCHAIN=auto
EOF
expect_failure_matching \
  "разрыв имени ENV-инструкции в deploy Dockerfile" \
  "deploy/images/agent-runner/Dockerfile: final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
  "$guard" --root "$deploy_split_instruction" --static-only

deploy_split_key="$temp_root/deploy-split-key"
copy_fixture "$deploy_split_key"
cat >>"$deploy_split_key/deploy/images/agent-runner/Dockerfile" <<'EOF'

ENV GOTOO\
LCHAIN=auto
EOF
expect_failure_matching \
  "разрыв имени GOTOOLCHAIN в deploy Dockerfile" \
  "deploy/images/agent-runner/Dockerfile: final runtime stage должен задавать GOTOOLCHAIN=local только в bootstrap и final ENV" \
  "$guard" --root "$deploy_split_key" --static-only

services_three_slash_override="$temp_root/services-three-slash-override"
copy_fixture "$services_three_slash_override"
cat >>"$services_three_slash_override/services/jobs/agent-runner/Dockerfile" <<'EOF'

RUN true \\\
ENV GOTOOLCHAIN=auto
EOF
expect_failure_matching \
  "три завершающих backslash перед поздним GOTOOLCHAIN=auto в services Dockerfile" \
  "services/jobs/agent-runner/Dockerfile: final runtime stage содержит write-capable RUN после обязательной stdout verification" \
  "$guard" --root "$services_three_slash_override" --static-only

deploy_three_slash_override="$temp_root/deploy-three-slash-override"
copy_fixture "$deploy_three_slash_override"
cat >>"$deploy_three_slash_override/deploy/images/agent-runner/Dockerfile" <<'EOF'

RUN true \\\
ENV GOTOOLCHAIN=auto
EOF
expect_failure_matching \
  "три завершающих backslash перед поздним GOTOOLCHAIN=auto в deploy Dockerfile" \
  "deploy/images/agent-runner/Dockerfile: final runtime stage содержит write-capable RUN после обязательной stdout verification" \
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

echo "PASS: Go toolchain contract проверяет final tools provenance, пустой BuildKit attrs allowlist, Kaniko stages, trusted GOROOT/compiler probe и hostile mutations"

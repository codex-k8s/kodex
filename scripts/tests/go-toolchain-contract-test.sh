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
  scripts/internal/go-toolchain-guard.go
  scripts/k8s/install-bot-service.sh
  scripts/lib/env.sh
  scripts/remote/install-bot-service.sh
  deploy/k8s/bot-service/kaniko-job.yaml.tpl
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
  chmod --reference="$target" "$target.tmp"
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
  chmod --reference="$target" "$target.tmp"
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
  chmod --reference="$target" "$target.tmp"
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
  chmod --reference="$target" "$target.tmp"
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
  chmod --reference="$target" "$target.tmp"
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
  chmod --reference="$target" "$target.tmp"
  mv "$target.tmp" "$target"
}

mutate_final_installer_tail() {
  local target="$1"
  local profile_index="$2"
  local mutation="$3"

  if [[ "$profile_index" == 0 ]]; then
    insert_after_exact_instruction \
      "$target" \
      $'\tcodex --version >/dev/null; \\' \
      $'\t'"$mutation"$'; \\'
    return
  fi
  replace_exact_instruction \
    "$target" \
    '  && codex --version >/dev/null' \
    $'  && codex --version >/dev/null \\\n  && '"$mutation"
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
  chmod --reference="$target" "$target.tmp"
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
  chmod --reference="$target" "$target.tmp"
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
  chmod --reference="$target" "$target.tmp"
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
  chmod --reference="$target" "$target.tmp"
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

expect_status() {
  local description="$1"
  local expected_status="$2"
  local actual_status
  shift 2

  set +e
  "$@" >"$temp_root/output" 2>&1
  actual_status=$?
  set -e
  if [[ "$actual_status" != "$expected_status" ]]; then
    echo "FAIL: $description завершился с exit $actual_status вместо $expected_status" >&2
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
guard_copy='COPY --chmod=0555 --from=0 /out/mattercodex-go-toolchain-guard /mattercodex-go-toolchain-guard'
clean_runs=(
  'RUN ["/mattercodex-go-toolchain-guard", "prepare", "services"]'
  'RUN ["/mattercodex-go-toolchain-guard", "prepare", "deploy"]'
)
runtime_copy='COPY --from=0 /usr/local/go/ /usr/local/go/'
verify_runs=(
  'RUN ["/mattercodex-go-toolchain-guard", "install", "services", "/usr/local/go/bin/go"]'
  'RUN ["/mattercodex-go-toolchain-guard", "install", "deploy", "/usr/local/go/bin/go"]'
)
verify_run_prefixes=(
  'RUN ["/mattercodex-go-toolchain-guard", "install", "services", \'
  'RUN ["/mattercodex-go-toolchain-guard", "install", "deploy", \'
)
go_tools_copies=('COPY --from=1 /out/mattercodex-protected/ /usr/local/bin/' 'COPY --from=1 /out/mattercodex-protected/ /usr/local/bin/')
final_artifact_copies=(
  'COPY --from=1 /out/mattercodex-protected/ /opt/mattercodex/protected-artifacts/'
  'COPY --from=1 /out/mattercodex-protected/ /opt/mattercodex/protected-artifacts/'
)
protected_runtime_copies=(
  'COPY --from=0 /out/mattercodex-go-toolchain-guard /opt/mattercodex/protected-artifacts/mattercodex-init'
  'COPY --from=0 /bin/busybox /opt/mattercodex/protected-artifacts/mattercodex-shell'
)
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
  final_artifact_copy="${final_artifact_copies[$index]}"
  protected_runtime_copy="${protected_runtime_copies[$index]}"
  clean_run="${clean_runs[$index]}"
  verify_run="${verify_runs[$index]}"

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

  topology_mutations=(
    'rm -rf /usr/local/bin && ln -s /tmp /usr/local/bin'
    'rm -rf /usr/local/bin/goose && ln -s /tmp/mattercodex-goose /usr/local/bin/goose'
    'rm -rf /usr/local/bin/goose && ln /bin/true /usr/local/bin/goose'
    'rm -rf /usr/local/bin/goose && mkdir /usr/local/bin/goose'
    'chmod 0777 /usr/local/bin'
  )
  topology_names=(directory-symlink leaf-symlink hardlink directory-as-file writable-mode)
  for topology_index in "${!topology_mutations[@]}"; do
    hostile_topology="$temp_root/$name-${topology_names[$topology_index]}"
    copy_fixture "$hostile_topology"
    mutate_final_installer_tail \
      "$hostile_topology/$path" \
      "$index" \
      "${topology_mutations[$topology_index]}"
    expect_success \
      "trusted prepare/install tail остаётся обязательным при ${topology_names[$topology_index]} mutation без изменения instruction counts в $name Dockerfile" \
      "$guard" --root "$hostile_topology" --static-only
  done

  if [[ "$name" == services ]]; then
    poisoned_final_runner="$temp_root/services-poisoned-final-runner"
    copy_fixture "$poisoned_final_runner"
    mutate_final_installer_tail \
      "$poisoned_final_runner/$path" \
      "$index" \
      'cp /bin/true /usr/local/bin/matter-codex-agent-runner'
    expect_success \
      "единый committed artifact tail восстанавливает runner после подмены внутри installer RUN" \
      "$guard" --root "$poisoned_final_runner" --static-only

    symlinked_final_runner="$temp_root/services-symlinked-final-runner"
    copy_fixture "$symlinked_final_runner"
    mutate_final_installer_tail \
      "$symlinked_final_runner/$path" \
      "$index" \
      'rm -f /usr/local/bin/matter-codex-agent-runner && ln -s /tmp/matter-codex-agent-runner /usr/local/bin/matter-codex-agent-runner'
    expect_success \
      "единый committed artifact tail восстанавливает runner symlink после installer RUN" \
      "$guard" --root "$symlinked_final_runner" --static-only

    poisoned_tini="$temp_root/services-poisoned-tini"
    copy_fixture "$poisoned_tini"
    mutate_final_installer_tail \
      "$poisoned_tini/$path" \
      "$index" \
      'ln -sf /bin/true /sbin/tini'
    expect_success \
      "protected Go init удаляет mutable tini из effective entrypoint chain" \
      "$guard" --root "$poisoned_tini" --static-only

    symlinked_bootstrap_guard="$temp_root/services-symlinked-bootstrap-guard"
    copy_fixture "$symlinked_bootstrap_guard"
    mutate_final_installer_tail \
      "$symlinked_bootstrap_guard/$path" \
      "$index" \
      'ln -sf /sbin/tini /mattercodex-go-toolchain-guard'
    expect_success \
      "bootstrap guard runtime topology check остаётся обязательным при destination symlink mutation" \
      "$guard" --root "$symlinked_bootstrap_guard" --static-only
  else
    poisoned_path_shell="$temp_root/deploy-poisoned-path-shell"
    copy_fixture "$poisoned_path_shell"
    mutate_final_installer_tail \
      "$poisoned_path_shell/$path" \
      "$index" \
      'cp /bin/true /usr/local/bin/sh'
    expect_success \
      "protected absolute default shell не разрешается через hostile PATH entry" \
      "$guard" --root "$poisoned_path_shell" --static-only
  fi

  duplicate_final_tools_copy="$temp_root/$name-duplicate-final-tools-copy"
  copy_fixture "$duplicate_final_tools_copy"
  insert_before_exact_instruction "$duplicate_final_tools_copy/$path" "$guard_copy" "$go_tools_copy"
  expect_failure_matching \
    "final COPY Go tools продублирован в $name Dockerfile" \
    "$path: final runtime stage должен копировать committed runner/tools" \
    "$guard" --root "$duplicate_final_tools_copy" --static-only

  substituted_final_tools_source="$temp_root/$name-substituted-final-tools-source"
  copy_fixture "$substituted_final_tools_source"
  replace_exact_instruction \
    "$substituted_final_tools_source/$path" \
    "$final_artifact_copy" \
    'COPY --from=0 /out/mattercodex-protected/ /opt/mattercodex/protected-artifacts/'
  expect_failure_matching \
    "final COPY Go tools получает подменённый numeric source в $name Dockerfile" \
    "$path: final runtime stage содержит неразрешённую COPY-инструкцию вне точного списка разрешённых источников" \
    "$guard" --root "$substituted_final_tools_source" --static-only

  reordered_final_tools_copy="$temp_root/$name-reordered-final-tools-copy"
  copy_fixture "$reordered_final_tools_copy"
  swap_exact_instructions "$reordered_final_tools_copy/$path" "$go_tools_copy" "$guard_copy"
  expect_failure_matching \
    "final COPY Go tools переставлен после trusted guard COPY в $name Dockerfile" \
    "$path: первичный COPY Go tools должен предшествовать Kaniko-compatible bootstrap COPY" \
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

  non_root_user='USER 10001:10001'
  user_mutations=(
    'ARG MATTERCODEX_AGENT_RUNNER_UID=0'
    'arg MATTERCODEX_AGENT_RUNNER_GID=0'
    'ARG MATTERCODEX_AGENT_RUNNER_UID="0"'
    'ARG "MATTERCODEX_AGENT_RUNNER_GID"=0'
    $'ARG MATTERCODEX_AGENT_RUNNER_UID=\\\n0'
    'ENV MATTERCODEX_AGENT_RUNNER_UID=0'
    'env MATTERCODEX_AGENT_RUNNER_GID="0"'
    'ARG MATTERCODEX_AGENT_RUNNER_UID=10001'
  )
  for user_mutation in "${user_mutations[@]}"; do
    user_key="$(printf '%s' "$user_mutation" | sha256sum | cut -c1-12)"
    hostile_user="$temp_root/$name-hostile-user-$user_key"
    copy_fixture "$hostile_user"
    insert_before_exact_instruction "$hostile_user/$path" "$non_root_user" "$user_mutation"
    expect_failure_matching \
      "late/duplicate/case/quoted/continued ARG/ENV runtime identity mutation отклонена в $name Dockerfile" \
      "$path" \
      "$guard" --root "$hostile_user" --static-only
  done

  parameterized_user="$temp_root/$name-parameterized-user"
  copy_fixture "$parameterized_user"
  replace_exact_instruction \
    "$parameterized_user/$path" \
    "$non_root_user" \
    'USER ${MATTERCODEX_AGENT_RUNNER_UID}:${MATTERCODEX_AGENT_RUNNER_GID}'
  expect_failure_matching \
    "параметризованный final USER не считается exact effective non-root config в $name Dockerfile" \
    "$path: final runtime stage должен закреплять effective USER точной literal инструкцией USER 10001:10001" \
    "$guard" --root "$parameterized_user" --static-only

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
    "$path: final runtime stage должен копировать committed runner/tools" \
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
    "$path: final runtime stage должен закреплять effective USER точной literal инструкцией USER 10001:10001" \
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
    "$path: trusted guard COPY, topology prepare, final Go COPY, protected staging COPY, protected runtime executable COPY, final ENV и install/verification должны идти непосредственно друг за другом" \
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

services_tini_entrypoint="$temp_root/services-tini-entrypoint"
copy_fixture "$services_tini_entrypoint"
replace_exact_instruction \
  "$services_tini_entrypoint/services/jobs/agent-runner/Dockerfile" \
  'ENTRYPOINT ["/usr/local/bin/mattercodex-init", "entrypoint", "/usr/local/bin/matter-codex-agent-runner"]' \
  'ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/matter-codex-agent-runner"]'
expect_failure_matching \
  "mutable tini нельзя вернуть в effective services entrypoint chain" \
  "после обязательной stdout verification разрешён только точный metadata tail" \
  "$guard" --root "$services_tini_entrypoint" --static-only

services_wrong_runner_entrypoint="$temp_root/services-wrong-runner-entrypoint"
copy_fixture "$services_wrong_runner_entrypoint"
replace_exact_instruction \
  "$services_wrong_runner_entrypoint/services/jobs/agent-runner/Dockerfile" \
  'ENTRYPOINT ["/usr/local/bin/mattercodex-init", "entrypoint", "/usr/local/bin/matter-codex-agent-runner"]' \
  'ENTRYPOINT ["/usr/local/bin/mattercodex-init", "entrypoint", "/usr/local/bin/attacker-runner"]'
expect_failure_matching \
  "protected services entrypoint закрепляет exact runner path" \
  "после обязательной stdout verification разрешён только точный metadata tail" \
  "$guard" --root "$services_wrong_runner_entrypoint" --static-only

for hostile_cmd in 'CMD ["sh"]' 'CMD ["/usr/local/bin/sh"]'; do
  deploy_hostile_cmd="$temp_root/deploy-hostile-cmd-$(printf '%s' "$hostile_cmd" | sha256sum | cut -c1-12)"
  copy_fixture "$deploy_hostile_cmd"
  replace_exact_instruction \
    "$deploy_hostile_cmd/deploy/images/agent-runner/Dockerfile" \
    'CMD ["/usr/local/bin/mattercodex-shell", "sh"]' \
    "$hostile_cmd"
  expect_failure_matching \
    "deploy effective CMD не допускает PATH lookup или uncommitted absolute shell" \
    "после обязательной stdout verification разрешён только точный metadata tail" \
    "$guard" --root "$deploy_hostile_cmd" --static-only
done

unsafe_local_build_input="$temp_root/unsafe-local-build-input"
copy_fixture "$unsafe_local_build_input"
replace_exact_instruction "$unsafe_local_build_input/scripts/k8s/install-bot-service.sh" "    --frontend-attrs-json '{}'" '    --build-context golang:1.26.5-alpine=docker-image://attacker.invalid/golang:latest'
expect_failure_matching "локальный supported BuildKit entrypoint получает named-context override exact base name" "scripts/k8s/install-bot-service.sh изменён без обновления полного source commitment" "$guard" --root "$unsafe_local_build_input" --static-only

direct_local_build_input="$temp_root/direct-local-build-input"
copy_fixture "$direct_local_build_input"
printf '\ndocker build -f "$REPO_ROOT/services/jobs/agent-runner/Dockerfile" "$REPO_ROOT"\n' >>"$direct_local_build_input/scripts/k8s/install-bot-service.sh"
expect_failure_matching "локальный supported entrypoint не может обойти защищённый wrapper прямым docker build" "scripts/k8s/install-bot-service.sh изменён без обновления полного source commitment" "$guard" --root "$direct_local_build_input" --static-only

dead_code_wrapper="$temp_root/dead-code-wrapper"
copy_fixture "$dead_code_wrapper"
replace_exact_instruction \
  "$dead_code_wrapper/scripts/k8s/install-bot-service.sh" \
  'if [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "${MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE:-true}"; then' \
  'if false && [ "$DRY_RUN_MODE" = "none" ] && mattercodex_bool "${MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE:-true}"; then'
printf '\ndocker build --build-context golang:1.26.5-alpine=docker-image://attacker.invalid/golang:latest -f "$REPO_ROOT/services/jobs/agent-runner/Dockerfile" "$REPO_ROOT"\n' >>"$dead_code_wrapper/scripts/k8s/install-bot-service.sh"
expect_failure_matching "wrapper нельзя сохранить только в dead code при active direct build" "scripts/k8s/install-bot-service.sh изменён без обновления полного source commitment" "$guard" --root "$dead_code_wrapper" --static-only

installer_bypass_payloads=(
  'builder=docker; "$builder" bu"ild" --build-context golang:1.26.5-alpine=attacker.invalid/golang .'
  $'docker bu\\\nild --build-context golang:1.26.5-alpine=attacker.invalid/golang .'
  'docker() { command docker build --build-context golang:1.26.5-alpine=attacker.invalid/golang .; }; docker'
  'PATH=/tmp/attacker:$PATH DOCKER_BUILDKIT=1 docker build --build-context golang:1.26.5-alpine=attacker.invalid/golang .'
  '"docker" "build" "--build-context=golang:1.26.5-alpine=attacker.invalid/golang" .'
  'set -- docker build --build-context golang:1.26.5-alpine=attacker.invalid/golang .; "$@"'
)
installer_bypass_names=(obfuscated split-token function-override path-env quoted positional)
for installer_index in 0 1; do
  if [[ "$installer_index" == 0 ]]; then
    installer_path=scripts/k8s/install-bot-service.sh
  else
    installer_path=scripts/remote/install-bot-service.sh
  fi
  for bypass_index in "${!installer_bypass_payloads[@]}"; do
    bypass_fixture="$temp_root/installer-$installer_index-${installer_bypass_names[$bypass_index]}"
    copy_fixture "$bypass_fixture"
    printf '\n%s\n' "${installer_bypass_payloads[$bypass_index]}" >>"$bypass_fixture/$installer_path"
    expect_failure_matching \
      "полный source commitment отклоняет ${installer_bypass_names[$bypass_index]} direct builder bypass в $installer_path" \
      "$installer_path изменён без обновления полного source commitment" \
      "$guard" --root "$bypass_fixture" --static-only
  done
done

unsafe_remote_build_input="$temp_root/unsafe-remote-build-input"
copy_fixture "$unsafe_remote_build_input"
replace_exact_instruction "$unsafe_remote_build_input/scripts/remote/install-bot-service.sh" "        --frontend-attrs-json '{}'\" </dev/null" "        --frontend-attrs-json '{\"context:golang:1.26.5-alpine\":\"docker-image://attacker.invalid/golang:latest\"}'\" </dev/null"
expect_failure_matching "remote supported BuildKit entrypoint получает frontend attrs с override exact base name" "scripts/remote/install-bot-service.sh изменён без обновления полного source commitment" "$guard" --root "$unsafe_remote_build_input" --static-only

mutated_build_wrapper="$temp_root/mutated-build-wrapper"
copy_fixture "$mutated_build_wrapper"
printf '\n# unsafe pass-through\n' >>"$mutated_build_wrapper/scripts/build-agent-runner-image.sh"
expect_failure_matching "защищённый BuildKit input wrapper изменён без обновления контракта" "scripts/build-agent-runner-image.sh изменён без обновления полного source commitment" "$guard" --root "$mutated_build_wrapper" --static-only

build_wrapper="$repo_root/scripts/build-agent-runner-image.sh"
fake_builder_bin="$temp_root/fake-builder-bin"
builder_capture="$temp_root/builder-argv"
mkdir -p "$fake_builder_bin"
for builder_name in docker nerdctl; do
  cat >"$fake_builder_bin/$builder_name" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$@" >"$MATTERCODEX_BUILDER_CAPTURE"
if [[ -n "${MATTERCODEX_BUILDER_ENV_CAPTURE:-}" ]]; then
  printf '%s\n' "${BUILDKIT_SYNTAX-unset}" >"$MATTERCODEX_BUILDER_ENV_CAPTURE"
fi
EOF
  chmod +x "$fake_builder_bin/$builder_name"
done

hostile_bash_env="$temp_root/hostile-bash-env"
cat >"$hostile_bash_env" <<'EOF'
type() { return 0; }
unset() { return 0; }
exec() {
  printf 'startup hook reached fake builder\n' >"$MATTERCODEX_BUILDER_CAPTURE"
  return 0
}
EOF
for startup_variable in BASH_ENV ENV; do
  rm -f "$builder_capture"
  expect_failure_matching \
    "direct wrapper отклоняет hostile $startup_variable до startup hook и builder" \
    "BASH_ENV и ENV запрещены" \
    env PATH="$fake_builder_bin:$PATH" "$startup_variable=$hostile_bash_env" MATTERCODEX_BUILDER_CAPTURE="$builder_capture" \
    "$build_wrapper" --builder docker --context "$repo_root" --dockerfile "$repo_root/services/jobs/agent-runner/Dockerfile" --tag mattercodex.invalid/agent-runner:test --frontend-attrs-json '{}'
  [[ ! -e "$builder_capture" ]] || {
    echo "FAIL: hostile $startup_variable достиг fake builder через direct wrapper" >&2
    exit 1
  }
done

for hostile_function_name in type unset exec; do
  eval "$hostile_function_name() { printf 'exported function invoked\\n' >\\\"\$MATTERCODEX_BUILDER_CAPTURE\\\"; return 0; }"
  export -f "$hostile_function_name"
  rm -f "$builder_capture"
  expect_failure_matching \
    "direct wrapper отклоняет exported $hostile_function_name function до builder" \
    "экспортированные shell functions запрещены" \
    env PATH="$fake_builder_bin:$PATH" MATTERCODEX_BUILDER_CAPTURE="$builder_capture" \
    "$build_wrapper" --builder docker --context "$repo_root" --dockerfile "$repo_root/services/jobs/agent-runner/Dockerfile" --tag mattercodex.invalid/agent-runner:test --frontend-attrs-json '{}'
  builtin unset -f "$hostile_function_name"
  [[ ! -e "$builder_capture" ]] || {
    echo "FAIL: exported $hostile_function_name function достиг fake builder" >&2
    exit 1
  }
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

builder_env_capture="$temp_root/builder-env"
builder_function_marker="$temp_root/builder-function-marker"
export builder_function_marker
docker() {
  printf 'function override invoked\n' >"$builder_function_marker"
  return 99
}
export -f docker
rm -f "$builder_capture"
expect_failure_matching \
  "wrapper fail-closed отклоняет exported function namespace до builder" \
  "экспортированные shell functions запрещены" \
  env PATH="$fake_builder_bin:$PATH" BUILDKIT_SYNTAX=attacker.invalid/frontend:latest MATTERCODEX_BUILDER_CAPTURE="$builder_capture" MATTERCODEX_BUILDER_ENV_CAPTURE="$builder_env_capture" \
  "$build_wrapper" --tag mattercodex.invalid/agent-runner:test --dockerfile "$repo_root/services/jobs/agent-runner/Dockerfile" --context "$repo_root" --builder docker --frontend-attrs-json '{}'
unset -f docker
[[ ! -e "$builder_function_marker" && ! -e "$builder_capture" ]] || {
  echo "FAIL: wrapper вызвал function override или fake builder при exported function namespace" >&2
  exit 1
}
expect_success \
  "wrapper очищает BUILDKIT_SYNTAX перед physical builder" \
  env PATH="$fake_builder_bin:$PATH" BUILDKIT_SYNTAX=attacker.invalid/frontend:latest MATTERCODEX_BUILDER_CAPTURE="$builder_capture" MATTERCODEX_BUILDER_ENV_CAPTURE="$builder_env_capture" \
  "$build_wrapper" --tag mattercodex.invalid/agent-runner:test --dockerfile "$repo_root/services/jobs/agent-runner/Dockerfile" --context "$repo_root" --builder docker --frontend-attrs-json '{}'
[[ "$(cat "$builder_env_capture")" == unset ]] || {
  echo "FAIL: wrapper сохранил hostile BUILDKIT_SYNTAX" >&2
  exit 1
}

quoted_context="$temp_root/context with spaces"
ln -s "$repo_root" "$quoted_context"
expect_success \
  "wrapper сохраняет quoting и допускает безопасный positional order аргументов" \
  env PATH="$fake_builder_bin:$PATH" MATTERCODEX_BUILDER_CAPTURE="$builder_capture" \
  "$build_wrapper" --frontend-attrs-json '{}' --dockerfile "$quoted_context/services/jobs/agent-runner/Dockerfile" --tag mattercodex.invalid/agent-runner:quoted --context "$quoted_context" --builder docker
grep -Fqx -- "$quoted_context" "$builder_capture" || {
  echo "FAIL: wrapper потерял quoted build context" >&2
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

rm -f "$builder_capture"
expect_failure_matching \
  "hostile named-context option отклоняется до fake builder invocation" \
  "named contexts и произвольные BuildKit frontend attrs запрещены" \
  env PATH="$fake_builder_bin:$PATH" MATTERCODEX_BUILDER_CAPTURE="$builder_capture" \
  "$build_wrapper" --builder docker --context "$repo_root" --dockerfile "$repo_root/services/jobs/agent-runner/Dockerfile" --tag mattercodex.invalid/agent-runner:test --build-context=golang:1.26.5-alpine=docker-image://attacker.invalid/golang:latest
[[ ! -e "$builder_capture" ]] || {
  echo "FAIL: hostile named-context option достиг fake builder" >&2
  exit 1
}

cat >"$fake_builder_bin/envsubst" <<'EOF'
#!/usr/bin/env bash
cat
EOF
cat >"$fake_builder_bin/kubectl" <<'EOF'
#!/usr/bin/env bash
if [[ "$*" == *jsonpath* ]]; then
  printf 'synthetic:1'
fi
EOF
chmod +x "$fake_builder_bin/envsubst" "$fake_builder_bin/kubectl"

installer_env="$temp_root/installer.env"
cat >"$installer_env" <<'EOF'
TARGET_HOST=synthetic.invalid
TARGET_PORT=22
TARGET_ROOT_USER=synthetic
TARGET_ROOT_SSH_KEY=/tmp/synthetic-key
OPERATOR_USER=synthetic
OPERATOR_SSH_PUBKEY_PATH=/tmp/synthetic.pub
PRODUCTION_NAMESPACE=mattermost
PRODUCTION_DOMAIN=example.invalid
PUBLIC_BASE_URL=https://mattermost.example.invalid
LETSENCRYPT_EMAIL=synthetic@example.invalid
MATTERCODEX_REMOTE_KUBECTL=kubectl
MATTERCODEX_BOT_SERVICE_BUILD_IMAGE=false
MATTERCODEX_AGENT_RUNNER_BUILD_IMAGE=true
MATTERCODEX_AGENT_RUNNER_IMAGE=mattercodex.invalid/agent-runner:test
MATTERCODEX_BOT_SERVICE_IMAGE=mattercodex.invalid/bot-service:test
MATTERCODEX_CODEX_PACKAGE=@openai/codex@0.144.1
MATTERCODEX_IMAGE_BUILD_STRATEGY=docker
MATTERCODEX_RUNTIME_ENABLED=false
EOF

local_installer_capture="$temp_root/local-installer-builder-argv"
local_render_dir="$temp_root/local-render"
mkdir -p "$local_render_dir"
for startup_variable in BASH_ENV ENV; do
  rm -f "$local_installer_capture"
  expect_failure_matching \
    "local installer отклоняет hostile $startup_variable до trusted logic и builder" \
    "BASH_ENV и ENV запрещены" \
    env PATH="$fake_builder_bin:$PATH" HOME="$temp_root" "$startup_variable=$hostile_bash_env" MATTERCODEX_BUILDER_CAPTURE="$local_installer_capture" \
    "$repo_root/scripts/k8s/install-bot-service.sh" --env-file "$installer_env" --apply --render-dir "$local_render_dir"
  [[ ! -e "$local_installer_capture" ]] || {
    echo "FAIL: hostile $startup_variable достиг builder через local installer" >&2
    exit 1
  }
done
for hostile_function_name in type unset exec; do
  eval "$hostile_function_name() { printf 'exported function invoked\\n' >\\\"\$MATTERCODEX_BUILDER_CAPTURE\\\"; return 0; }"
  export -f "$hostile_function_name"
  rm -f "$local_installer_capture"
  expect_failure_matching \
    "local installer отклоняет exported $hostile_function_name function до builder" \
    "экспортированные shell functions запрещены" \
    env PATH="$fake_builder_bin:$PATH" HOME="$temp_root" MATTERCODEX_BUILDER_CAPTURE="$local_installer_capture" \
    "$repo_root/scripts/k8s/install-bot-service.sh" --env-file "$installer_env" --apply --render-dir "$local_render_dir"
  builtin unset -f "$hostile_function_name"
  [[ ! -e "$local_installer_capture" ]] || {
    echo "FAIL: exported $hostile_function_name достиг builder через local installer" >&2
    exit 1
  }
done
expect_success \
  "локальный installer active Docker flow исполняет только repository wrapper и physical fake builder" \
  env -i PATH="$fake_builder_bin:$PATH" HOME="$temp_root" MATTERCODEX_BUILDER_CAPTURE="$local_installer_capture" \
  "$repo_root/scripts/k8s/install-bot-service.sh" --env-file "$installer_env" --apply --render-dir "$local_render_dir"
[[ "$(grep -c '^build$' "$local_installer_capture")" == 1 ]] || {
  echo "FAIL: локальный installer не выполнил ровно один wrapped agent-runner build" >&2
  exit 1
}
if grep -Eq -- '(^--build-context|^--opt|context:)' "$local_installer_capture"; then
  echo "FAIL: локальный installer передал hostile named context fake builder-у" >&2
  exit 1
fi

cat >"$fake_builder_bin/ssh" <<'EOF'
#!/usr/bin/env bash
remote_command="${!#}"
case "$remote_command" in
  *'if command -v docker'*'command -v nerdctl'*)
    printf '%s\n' "$MATTERCODEX_FAKE_REMOTE_BUILDER"
    ;;
  *'status.conditions'*'Complete'*)
    : >"$MATTERCODEX_FAKE_KANIKO_MARKER"
    printf 'True'
    ;;
  *'rm -rf '*'/tmp/matter-codex-agent-runner-build'*'tar -xzf -'*)
    rm -rf "$MATTERCODEX_FAKE_REMOTE_DIR"
    mkdir -p "$MATTERCODEX_FAKE_REMOTE_DIR"
    tar -xzf - -C "$MATTERCODEX_FAKE_REMOTE_DIR"
    ;;
  *'./scripts/build-agent-runner-image.sh'*)
    remote_command="${remote_command//\/tmp\/matter-codex-agent-runner-build/$MATTERCODEX_FAKE_REMOTE_DIR}"
    bash -c "$remote_command"
    ;;
  *'tar -xzf -'*)
    cat >/dev/null
    ;;
  *)
    if [[ -n "${MATTERCODEX_FAKE_KANIKO_MANIFEST_CAPTURE:-}" ]]; then
      cat >>"$MATTERCODEX_FAKE_KANIKO_MANIFEST_CAPTURE" || true
    else
      cat >/dev/null || true
    fi
    ;;
esac
EOF
chmod +x "$fake_builder_bin/ssh"

remote_startup_capture="$temp_root/remote-startup-builder-argv"
for startup_variable in BASH_ENV ENV; do
  rm -f "$remote_startup_capture"
  expect_failure_matching \
    "remote installer отклоняет hostile $startup_variable до trusted logic и builder" \
    "BASH_ENV и ENV запрещены" \
    env PATH="$fake_builder_bin:$PATH" HOME="$temp_root" "$startup_variable=$hostile_bash_env" \
    MATTERCODEX_BUILDER_CAPTURE="$remote_startup_capture" \
    MATTERCODEX_FAKE_REMOTE_BUILDER=docker \
    MATTERCODEX_FAKE_REMOTE_DIR="$temp_root/remote-startup-root" \
    MATTERCODEX_FAKE_KANIKO_MARKER="$temp_root/remote-startup-kaniko-marker" \
    "$repo_root/scripts/remote/install-bot-service.sh" --env-file "$installer_env" --apply --build-only --render-dir "$temp_root/remote-startup-render"
  [[ ! -e "$remote_startup_capture" ]] || {
    echo "FAIL: hostile $startup_variable достиг builder через remote installer" >&2
    exit 1
  }
done
for hostile_function_name in type unset exec; do
  eval "$hostile_function_name() { printf 'exported function invoked\\n' >\\\"\$MATTERCODEX_BUILDER_CAPTURE\\\"; return 0; }"
  export -f "$hostile_function_name"
  rm -f "$remote_startup_capture"
  expect_failure_matching \
    "remote installer отклоняет exported $hostile_function_name function до builder" \
    "экспортированные shell functions запрещены" \
    env PATH="$fake_builder_bin:$PATH" HOME="$temp_root" \
    MATTERCODEX_BUILDER_CAPTURE="$remote_startup_capture" \
    MATTERCODEX_FAKE_REMOTE_BUILDER=docker \
    MATTERCODEX_FAKE_REMOTE_DIR="$temp_root/remote-startup-root" \
    MATTERCODEX_FAKE_KANIKO_MARKER="$temp_root/remote-startup-kaniko-marker" \
    "$repo_root/scripts/remote/install-bot-service.sh" --env-file "$installer_env" --apply --build-only --render-dir "$temp_root/remote-startup-render"
  builtin unset -f "$hostile_function_name"
  [[ ! -e "$remote_startup_capture" ]] || {
    echo "FAIL: exported $hostile_function_name достиг builder через remote installer" >&2
    exit 1
  }
done

for remote_builder in docker nerdctl; do
  remote_installer_capture="$temp_root/remote-$remote_builder-builder-argv"
  remote_render_dir="$temp_root/remote-$remote_builder-render"
  fake_remote_dir="$temp_root/remote-$remote_builder-root"
  mkdir -p "$remote_render_dir"
  expect_success \
    "remote installer active $remote_builder flow исполняет archived repository wrapper и physical fake builder" \
    env -i PATH="$fake_builder_bin:$PATH" HOME="$temp_root" \
    MATTERCODEX_BUILDER_CAPTURE="$remote_installer_capture" \
    MATTERCODEX_FAKE_REMOTE_BUILDER="$remote_builder" \
    MATTERCODEX_FAKE_REMOTE_DIR="$fake_remote_dir" \
    MATTERCODEX_FAKE_KANIKO_MARKER="$temp_root/unused-kaniko-marker" \
    "$repo_root/scripts/remote/install-bot-service.sh" --env-file "$installer_env" --apply --build-only --render-dir "$remote_render_dir"
  [[ "$(grep -c '^build$' "$remote_installer_capture")" == 1 ]] || {
    echo "FAIL: remote $remote_builder installer не выполнил ровно один wrapped agent-runner build" >&2
    exit 1
  }
  if grep -Eq -- '(^--build-context|^--opt|context:)' "$remote_installer_capture"; then
    echo "FAIL: remote $remote_builder installer передал hostile named context fake builder-у" >&2
    exit 1
  fi
  if [[ "$remote_builder" == nerdctl ]] && { [[ "$(sed -n '1p' "$remote_installer_capture")" != -n ]] || [[ "$(sed -n '2p' "$remote_installer_capture")" != k8s.io ]]; }; then
    echo "FAIL: remote nerdctl installer потерял namespace k8s.io" >&2
    exit 1
  fi
done

kaniko_env="$temp_root/kaniko-installer.env"
cp "$installer_env" "$kaniko_env"
printf '%s\n' 'MATTERCODEX_IMAGE_BUILD_STRATEGY=kaniko' 'MATTERCODEX_IMAGE_REGISTRY_MANAGED=false' >>"$kaniko_env"
kaniko_marker="$temp_root/kaniko-functional-marker"
kaniko_builder_capture="$temp_root/kaniko-builder-must-not-run"
kaniko_manifest_capture="$temp_root/kaniko-rendered-manifests"
kaniko_render_dir="$temp_root/kaniko-render"
mkdir -p "$kaniko_render_dir"
for hostile_kaniko_input in \
  MATTERCODEX_KANIKO_EXTRA_ARGS_YAML \
  MATTERCODEX_KANIKO_CACHE \
  MATTERCODEX_KANIKO_CACHE_RUN_LAYERS \
  MATTERCODEX_KANIKO_CACHE_COPY_LAYERS \
  MATTERCODEX_KANIKO_CACHE_REPO; do
  expect_failure_matching \
    "remote Kaniko flow fail-closed отклоняет hostile $hostile_kaniko_input" \
    "$hostile_kaniko_input не поддерживается" \
    env -i PATH="$fake_builder_bin:$PATH" HOME="$temp_root" \
    "$hostile_kaniko_input=attacker.invalid/cache" \
    MATTERCODEX_FAKE_REMOTE_BUILDER=none \
    MATTERCODEX_FAKE_REMOTE_DIR="$temp_root/kaniko-hostile-root" \
    MATTERCODEX_FAKE_KANIKO_MARKER="$temp_root/kaniko-hostile-marker" \
    "$repo_root/scripts/remote/install-bot-service.sh" --env-file "$kaniko_env" --apply --build-only --render-dir "$temp_root/kaniko-hostile-render"
done
expect_success \
  "default Kaniko installer flow остаётся исполняемым и не требует Docker/nerdctl wrapper" \
  env -i PATH="$fake_builder_bin:$PATH" HOME="$temp_root" \
  MATTERCODEX_BUILDER_CAPTURE="$kaniko_builder_capture" \
  MATTERCODEX_FAKE_REMOTE_BUILDER=none \
  MATTERCODEX_FAKE_REMOTE_DIR="$temp_root/kaniko-remote-root" \
  MATTERCODEX_FAKE_KANIKO_MARKER="$kaniko_marker" \
  MATTERCODEX_FAKE_KANIKO_MANIFEST_CAPTURE="$kaniko_manifest_capture" \
  "$repo_root/scripts/remote/install-bot-service.sh" --env-file "$kaniko_env" --apply --build-only --render-dir "$kaniko_render_dir"
[[ -e "$kaniko_marker" && ! -e "$kaniko_builder_capture" ]] || {
  echo "FAIL: Kaniko flow не завершил fake job либо вызвал Docker/nerdctl wrapper" >&2
  exit 1
}
for disabled_cache_arg in '--cache=false' '--cache-run-layers=false' '--cache-copy-layers=false'; do
  grep -Fq -- "$disabled_cache_arg" "$kaniko_manifest_capture" || {
    echo "FAIL: canonical rendered Kaniko job не содержит $disabled_cache_arg" >&2
    exit 1
  }
done
if grep -Eq -- '(^|[=\" ])--cache=true([\" ]|$)|--cache-repo' "$kaniko_manifest_capture"; then
  echo "FAIL: canonical rendered Kaniko job сохранил mutable cache input" >&2
  exit 1
fi

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
		"slices"
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
	var sourceCopies, workCopies, prepareChecks, installChecks, literalUsers int
	var bootstrapCopyIndex, guardCopyIndex, prepareIndex, runtimeCopyIndex, protectedCopyIndex, protectedRuntimeCopyIndex, finalEnvIndex, installIndex, initialToolsCopyIndex int
	finalEnvValue := ""
	profile := "deploy"
	if strings.Contains(path, "services/jobs") {
		profile = "services"
	}
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
				if typed.From == "1" && len(typed.SourcePaths) == 1 && typed.SourcePaths[0] == "/out/mattercodex-protected/" && typed.DestPath == "/usr/local/bin/" {
					initialToolsCopyIndex = commandIndex
				}
				if typed.From == "1" && len(typed.SourcePaths) == 1 && typed.SourcePaths[0] == "/out/mattercodex-protected/" && typed.DestPath == "/opt/mattercodex/protected-artifacts/" {
					protectedCopyIndex = commandIndex
				}
			if typed.From == "0" && len(typed.SourcePaths) == 1 && typed.SourcePaths[0] == "/usr/local/go/" && typed.DestPath == "/opt/mattercodex/bootstrap-go/" {
				bootstrapCopyIndex = commandIndex
			}
				if typed.From == "0" && len(typed.SourcePaths) == 1 && typed.SourcePaths[0] == "/out/mattercodex-go-toolchain-guard" && typed.DestPath == "/mattercodex-go-toolchain-guard" {
					guardCopyIndex = commandIndex
			}
				if typed.From == "0" && len(typed.SourcePaths) == 1 && typed.SourcePaths[0] == "/usr/local/go/" && typed.DestPath == "/usr/local/go/" {
					runtimeCopyIndex = commandIndex
				}
				if profile == "services" && typed.From == "0" && len(typed.SourcePaths) == 1 && typed.SourcePaths[0] == "/out/mattercodex-go-toolchain-guard" && typed.DestPath == "/opt/mattercodex/protected-artifacts/mattercodex-init" {
					protectedRuntimeCopyIndex = commandIndex
				}
				if profile == "deploy" && typed.From == "0" && len(typed.SourcePaths) == 1 && typed.SourcePaths[0] == "/bin/busybox" && typed.DestPath == "/opt/mattercodex/protected-artifacts/mattercodex-shell" {
					protectedRuntimeCopyIndex = commandIndex
				}
		case *instructions.RunCommand:
			if typed.PrependShell {
				continue
			}
			joined := strings.Join(typed.CmdLine, "\x00")
				if joined == "/mattercodex-go-toolchain-guard\x00prepare\x00"+profile {
					prepareChecks++
					prepareIndex = commandIndex
				}
				if joined == "/mattercodex-go-toolchain-guard\x00install\x00"+profile+"\x00/usr/local/go/bin/go" {
					installChecks++
					installIndex = commandIndex
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
			case *instructions.UserCommand:
				if typed.User == "10001:10001" {
					literalUsers++
				}
			}
		}
		if sourceCopies != 4 || workCopies != 2 || prepareChecks != 1 || installChecks != 1 || literalUsers != 1 {
			return fmt.Errorf("%s: parser contract source=%d work=%d prepare=%d install=%d user=%d", path, sourceCopies, workCopies, prepareChecks, installChecks, literalUsers)
		}
	expectedFinalEnv := "GOROOT=/usr/local/go GOENV=off GOFLAGS= GOTOOLCHAIN=local PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin"
	if strings.Contains(path, "services/jobs") {
		expectedFinalEnv += " PLAYWRIGHT_BROWSERS_PATH=/ms-playwright"
	}
			if initialToolsCopyIndex == 0 || protectedCopyIndex == 0 || protectedRuntimeCopyIndex == 0 || finalEnvValue != expectedFinalEnv {
				return fmt.Errorf("%s: parser не подтвердил committed runner/tools/runtime executable source и exact final environment", path)
			}
			if initialToolsCopyIndex >= bootstrapCopyIndex || guardCopyIndex+1 != prepareIndex || prepareIndex+1 != runtimeCopyIndex || runtimeCopyIndex+1 != protectedCopyIndex || protectedCopyIndex+1 != protectedRuntimeCopyIndex || protectedRuntimeCopyIndex+1 != finalEnvIndex || finalEnvIndex+1 != installIndex {
				return fmt.Errorf("%s: parser не подтвердил protected final source/order tail", path)
	}
	return nil
}

type effectiveConfig struct {
	user       string
	entrypoint []string
	cmd        []string
	env        []string
}

func convertWithContexts(data []byte, attrs map[string]string) ([]string, effectiveConfig, error) {
	caps := pb.Caps.CapSet(pb.Caps.All())
	probe := &gatewayProbe{opts: gateway.BuildOpts{
		Opts:    attrs,
		LLBCaps: caps,
		Caps:    gatewaypb.Caps.CapSet(gatewaypb.Caps.All()),
	}}
	client, err := dockerui.NewClient(probe)
	if err != nil {
		return nil, effectiveConfig{}, err
	}
	result, err := dockerfile2llb.Dockerfile2LLB(context.Background(), data, dockerfile2llb.ConvertOpt{
		Client:       client,
		MetaResolver: imageResolver{},
		LLBCaps:      &caps,
		AllStages:    true,
	})
	effective := effectiveConfig{}
	if result != nil && result.Image != nil {
		effective.user = result.Image.Config.User
		effective.entrypoint = result.Image.Config.Entrypoint
		effective.cmd = result.Image.Config.Cmd
		effective.env = result.Image.Config.Env
	}
	return probe.requests, effective, err
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
	requests, effective, err := convertWithContexts(data, map[string]string{})
	if err != nil {
		return fmt.Errorf("canonical empty-attrs Dockerfile2LLB %s: %w", path, err)
	}
	if len(requests) != 0 {
		return fmt.Errorf("%s: canonical protected stages вызвали external image requests: %v", path, requests)
	}
	if effective.user != "10001:10001" {
		return fmt.Errorf("%s: BuildKit effective image user=%q вместо 10001:10001", path, effective.user)
	}
	expectedPath := "PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin"
	if !slices.Contains(effective.env, expectedPath) {
		return fmt.Errorf("%s: BuildKit effective image env не содержит exact protected PATH", path)
	}
	if strings.Contains(path, "services/jobs") {
		expectedEntrypoint := []string{"/usr/local/bin/mattercodex-init", "entrypoint", "/usr/local/bin/matter-codex-agent-runner"}
		if !slices.Equal(effective.entrypoint, expectedEntrypoint) || len(effective.cmd) != 0 {
			return fmt.Errorf("%s: BuildKit effective entrypoint=%q cmd=%q", path, effective.entrypoint, effective.cmd)
		}
	} else {
		expectedCmd := []string{"/usr/local/bin/mattercodex-shell", "sh"}
		if !slices.Equal(effective.cmd, expectedCmd) || len(effective.entrypoint) != 0 {
			return fmt.Errorf("%s: BuildKit effective entrypoint=%q cmd=%q", path, effective.entrypoint, effective.cmd)
		}
	}
	for _, mutation := range []struct {
		occurrence int
		alias      string
	}{{1, "go-toolchain-source"}, {2, "go-tools"}} {
		mutated, err := replaceProtectedAlias(data, mutation.occurrence, mutation.alias)
		if err != nil {
			return err
		}
		requests, _, err := convertWithContexts(mutated, map[string]string{
			"context:scratch":             "docker-image://attacker.invalid/scratch:latest",
			"context:context":             "docker-image://attacker.invalid/context:latest",
			"context:go-toolchain-source": "docker-image://attacker.invalid/go:latest",
			"context:go-tools":            "docker-image://attacker.invalid/tools:latest",
		})
		if !errors.Is(err, errAttackerImageRequested) || len(requests) != 1 || !strings.Contains(requests[0], "attacker.invalid") {
			return fmt.Errorf("%s: ConvertOpt.Client не доказал substitution alias %s: requests=%v err=%v", path, mutation.alias, requests, err)
		}
	}
	requests, _, err = convertWithContexts(data, map[string]string{
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
	"slices"
	"strconv"
	"strings"

	"github.com/GoogleContainerTools/kaniko/pkg/commands"
	"github.com/GoogleContainerTools/kaniko/pkg/config"
	kanikodockerfile "github.com/GoogleContainerTools/kaniko/pkg/dockerfile"
	"github.com/GoogleContainerTools/kaniko/pkg/util"
	v1 "github.com/google/go-containerregistry/pkg/v1"
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
	if sourceCopies != 4 || workCopies != 2 {
		return fmt.Errorf("%s: Kaniko dependencies source=%d work=%d", path, sourceCopies, workCopies)
	}
	imageConfig := &v1.Config{}
	buildArgs := kanikodockerfile.NewBuildArgs(nil)
	for _, instruction := range kanikoStages[2].Commands {
		switch instruction.(type) {
		case *instructions.ArgCommand, *instructions.EnvCommand, *instructions.UserCommand, *instructions.CmdCommand, *instructions.EntrypointCommand:
			command, err := commands.GetCommand(instruction, util.FileContext{}, false, false, false)
			if err != nil {
				return fmt.Errorf("GetCommand %s: %w", path, err)
			}
			if err := command.ExecuteCommand(imageConfig, buildArgs); err != nil {
				return fmt.Errorf("ExecuteCommand %s: %w", path, err)
			}
		}
	}
	if imageConfig.User != "10001:10001" {
		return fmt.Errorf("%s: Kaniko effective image user=%q вместо 10001:10001", path, imageConfig.User)
	}
	expectedPath := "PATH=/usr/local/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin"
	if !slices.Contains(imageConfig.Env, expectedPath) {
		return fmt.Errorf("%s: Kaniko effective image env не содержит exact protected PATH", path)
	}
	if strings.Contains(path, "services/jobs") {
		expectedEntrypoint := []string{"/usr/local/bin/mattercodex-init", "entrypoint", "/usr/local/bin/matter-codex-agent-runner"}
		if !slices.Equal(imageConfig.Entrypoint, expectedEntrypoint) || len(imageConfig.Cmd) != 0 {
			return fmt.Errorf("%s: Kaniko effective entrypoint=%q cmd=%q", path, imageConfig.Entrypoint, imageConfig.Cmd)
		}
	} else {
		expectedCmd := []string{"/usr/local/bin/mattercodex-shell", "sh"}
		if !slices.Equal(imageConfig.Cmd, expectedCmd) || len(imageConfig.Entrypoint) != 0 {
			return fmt.Errorf("%s: Kaniko effective entrypoint=%q cmd=%q", path, imageConfig.Entrypoint, imageConfig.Cmd)
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
kaniko_source_dir="$(sed -n 's/^[[:space:]]*"Dir": "\(.*\)",$/\1/p' <<<"$kaniko_module_json")"
kaniko_cache_probe_root="$temp_root/kaniko-cache-probe"
mkdir -p "$kaniko_cache_probe_root"
cp -a "$kaniko_source_dir/." "$kaniko_cache_probe_root/"
chmod -R u+w "$kaniko_cache_probe_root"
cat >"$kaniko_cache_probe_root/pkg/executor/mattercodex_cache_contract_test.go" <<'EOF'
package executor

import (
	"testing"

	"github.com/GoogleContainerTools/kaniko/pkg/commands"
	"github.com/GoogleContainerTools/kaniko/pkg/config"
	"github.com/GoogleContainerTools/kaniko/pkg/dockerfile"
	"github.com/GoogleContainerTools/kaniko/pkg/util"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
)

func protectedRunCommand(t *testing.T) commands.DockerCommand {
	t.Helper()
	instruction := &instructions.RunCommand{ShellDependantCmdLine: instructions.ShellDependantCmdLine{
		CmdLine:      []string{"/mattercodex-go-toolchain-guard", "install", "services", "/usr/local/go/bin/go"},
		PrependShell: false,
	}}
	command, err := commands.GetCommand(instruction, util.FileContext{}, false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := command.(*commands.RunCommand); !ok {
		t.Fatalf("protected RUN получил исходный тип %T", command)
	}
	return command
}

func TestMatterCodexCacheDisabledPreservesProtectedRun(t *testing.T) {
	configFile := &v1.ConfigFile{}
	hostileCache := &fakeLayerCache{retrieve: true, img: empty.Image}
	builder := &stageBuilder{
		opts:       &config.KanikoOptions{Cache: false, CacheRunLayers: true, CacheRepo: "attacker.invalid/mutable-cache"},
		cf:         configFile,
		layerCache: hostileCache,
		args:       dockerfile.NewBuildArgs(nil),
		cmds:       []commands.DockerCommand{protectedRunCommand(t)},
	}
	if err := builder.optimize(CompositeCache{}, configFile.Config); err != nil {
		t.Fatal(err)
	}
	if len(hostileCache.receivedKeys) != 0 {
		t.Fatalf("cache disabled, но Kaniko обратился к hostile cache: %v", hostileCache.receivedKeys)
	}
	if _, ok := builder.cmds[0].(*commands.RunCommand); !ok {
		t.Fatalf("cache disabled заменил protected RUN на %T", builder.cmds[0])
	}

	enabledCache := &fakeLayerCache{retrieve: true, img: empty.Image}
	builder = &stageBuilder{
		opts:       &config.KanikoOptions{Cache: true, CacheRunLayers: true, CacheRepo: "attacker.invalid/mutable-cache"},
		cf:         configFile,
		layerCache: enabledCache,
		args:       dockerfile.NewBuildArgs(nil),
		cmds:       []commands.DockerCommand{protectedRunCommand(t)},
	}
	if err := builder.optimize(CompositeCache{}, configFile.Config); err != nil {
		t.Fatal(err)
	}
	if len(enabledCache.receivedKeys) != 1 {
		t.Fatalf("cache enabled не воспроизвёл hostile cache lookup: %v", enabledCache.receivedKeys)
	}
	if _, ok := builder.cmds[0].(*commands.CachingRunCommand); !ok {
		t.Fatalf("cache enabled не заменил protected RUN на CachingRunCommand: %T", builder.cmds[0])
	}
}
EOF
expect_success \
  "exact Kaniko v1.24.0 cache=false не заменяет protected guard через hostile CachingRunCommand" \
  env GOENV=off GOWORK=off go -C "$kaniko_cache_probe_root" test ./pkg/executor -run '^TestMatterCodexCacheDisabledPreservesProtectedRun$' -count=1
expect_success \
  "exact Kaniko v1.24.0 upstream CachingRunCommand извлекает cached layer вместо RUN" \
  env GOENV=off GOWORK=off go -C "$kaniko_cache_probe_root" test ./pkg/commands -run '^Test_CachingRunCommand_ExecuteCommand$' -count=1

trusted_guard_services_base64="$(sed -n "s/^[[:space:]]*&& printf '%s' '\([^']*\)'.*/\1/p" "$repo_root/services/jobs/agent-runner/Dockerfile")"
trusted_guard_deploy_base64="$(sed -n "s/^[[:space:]]*&& printf '%s' '\([^']*\)'.*/\1/p" "$repo_root/deploy/images/agent-runner/Dockerfile")"
[[ -n "$trusted_guard_services_base64" && "$trusted_guard_services_base64" == "$trusted_guard_deploy_base64" ]] || {
  echo "FAIL: обе Dockerfile должны собирать trusted guard из одного закреплённого source" >&2
  exit 1
}
trusted_guard_source="$temp_root/mattercodex-go-toolchain-guard.go"
trusted_guard_binary="$temp_root/mattercodex-go-toolchain-guard"
printf '%s' "$trusted_guard_services_base64" | base64 -d >"$trusted_guard_source"
cmp "$repo_root/scripts/internal/go-toolchain-guard.go" "$trusted_guard_source" || {
  echo "FAIL: embedded trusted guard source расходится с каноническим Go source" >&2
  exit 1
}
expect_success \
  "trusted Go toolchain guard собирается из фактического Dockerfile source" \
  env CGO_ENABLED=0 GOENV=off GOTOOLCHAIN=local GOWORK=off go build -trimpath -buildvcs=false -o "$trusted_guard_binary" "$trusted_guard_source"
expect_success \
  "trusted Go toolchain guard является Go artifact, а не no-op compiler output" \
  env GOENV=off GOTOOLCHAIN=local GOWORK=off go version -m "$trusted_guard_binary"
expect_success \
  "production guard закрепляет literal root UID" \
  grep -Fqx $'\ttrustedUID          = 0' "$trusted_guard_source"
expect_success \
  "production guard закрепляет literal root GID" \
  grep -Fqx $'\ttrustedGID          = 0' "$trusted_guard_source"
if [[ "$(id -u)" != 0 || "$(id -g)" != 0 ]]; then
  expect_failure_matching \
    "production prepare запрещён без effective root UID/GID" \
    "trusted guard requires uid:gid 0:0" \
    "$trusted_guard_binary" prepare deploy
fi

cleanup_root="$temp_root/cleanup-root"
cleanup_go_root="$cleanup_root/usr/local/go"
cleanup_bootstrap_root="$cleanup_root/opt/mattercodex/bootstrap-go"
cleanup_staging_root="$cleanup_root/opt/mattercodex/protected-artifacts"
cleanup_target_root="$cleanup_root/usr/local/bin"
cleanup_guard_source="$temp_root/mattercodex-go-toolchain-cleanup-guard.go"
cleanup_guard_binary="$cleanup_root/mattercodex-go-toolchain-guard"
cleanup_uid="$(id -u)"
cleanup_gid="$(id -g)"
mkdir -p "$cleanup_root"
sed \
  -e "s#/usr/local/go#$cleanup_go_root#g" \
  -e "s#/opt/mattercodex/bootstrap-go#$cleanup_bootstrap_root#g" \
  -e "s#/opt/mattercodex/protected-artifacts#$cleanup_staging_root#g" \
  -e "s#/usr/local/bin#$cleanup_target_root#g" \
  -e "s#protectedRoot       = \"/\"#protectedRoot       = \"$cleanup_root\"#" \
  -e "s#trustedUID          = 0#trustedUID          = $cleanup_uid#" \
  -e "s#trustedGID          = 0#trustedGID          = $cleanup_gid#" \
  "$trusted_guard_source" >"$cleanup_guard_source"
expect_success \
  "trusted cleaner собирается с изолированными behavioral destinations" \
  env CGO_ENABLED=0 GOENV=off GOTOOLCHAIN=local GOWORK=off go build -trimpath -buildvcs=false -o "$cleanup_guard_binary" "$cleanup_guard_source"
chmod 0555 "$cleanup_guard_binary"
mkdir -p "$cleanup_go_root/src/runtime" "$cleanup_bootstrap_root/src/runtime"
printf '%s\n' contaminated >"$cleanup_go_root/src/runtime/mattercodex-injected.go"
printf '%s\n' contaminated >"$cleanup_bootstrap_root/src/runtime/mattercodex-injected.go"
expect_success \
  "trusted prepare восстанавливает topology и удаляет destination-only GOROOT/bootstrap contamination" \
  "$cleanup_guard_binary" prepare deploy
[[ -d "$cleanup_go_root" && ! -e "$cleanup_go_root/src/runtime/mattercodex-injected.go" && ! -e "$cleanup_bootstrap_root" && -d "$cleanup_staging_root" && -d "$cleanup_target_root" ]] || {
  echo "FAIL: trusted prepare сохранил contamination или не восстановил protected topology" >&2
  exit 1
}

topology_tmp="$cleanup_root/tmp"
mkdir -p "$topology_tmp"
printf 'tmp target must survive\n' >"$topology_tmp/marker"
bootstrap_guard_backup="$topology_tmp/trusted-bootstrap-guard"
cp "$cleanup_guard_binary" "$bootstrap_guard_backup"
chmod 0555 "$bootstrap_guard_backup"
restore_bootstrap_guard() {
  rm -rf "$cleanup_guard_binary"
  cp "$bootstrap_guard_backup" "$cleanup_guard_binary"
  chmod 0555 "$cleanup_guard_binary"
}

rm -f "$cleanup_guard_binary"
ln -s "$bootstrap_guard_backup" "$cleanup_guard_binary"
expect_failure_matching \
  "bootstrap guard destination symlink отклоняется до topology prepare" \
  "not a regular non-symlink file" \
  "$cleanup_guard_binary" prepare deploy
restore_bootstrap_guard

rm -f "$cleanup_guard_binary"
ln "$bootstrap_guard_backup" "$cleanup_guard_binary"
expect_failure_matching \
  "bootstrap guard hardlink отклоняется до topology prepare" \
  "has 2 hardlinks" \
  "$cleanup_guard_binary" prepare deploy
restore_bootstrap_guard

rm -f "$cleanup_guard_binary"
mkdir "$cleanup_guard_binary"
expect_failure_matching \
  "bootstrap guard directory-as-file отклоняется до topology prepare" \
  "not a regular non-symlink file" \
  "$bootstrap_guard_backup" prepare deploy
restore_bootstrap_guard

chmod 0755 "$cleanup_guard_binary"
expect_failure_matching \
  "bootstrap guard owner-writable mode отклоняется до topology prepare" \
  "has mode 0755, want 0555" \
  "$cleanup_guard_binary" prepare deploy
restore_bootstrap_guard

cp /bin/true "$topology_tmp/hostile-bootstrap-guard"
chmod 0555 "$topology_tmp/hostile-bootstrap-guard"
rm -f "$cleanup_guard_binary"
cp "$topology_tmp/hostile-bootstrap-guard" "$cleanup_guard_binary"
chmod 0555 "$cleanup_guard_binary"
expect_failure_matching \
  "bootstrap guard digest mismatch отклоняется до topology prepare" \
  "running guard digest differs" \
  "$bootstrap_guard_backup" prepare deploy
restore_bootstrap_guard

chmod 0777 "$cleanup_root"
expect_failure_matching \
  "bootstrap guard writable parent topology отклоняется до prepare" \
  "is group/world-writable" \
  "$cleanup_guard_binary" prepare deploy
chmod 0755 "$cleanup_root"

rm -rf "$cleanup_target_root" "$cleanup_staging_root"
ln -s "$topology_tmp" "$cleanup_target_root"
ln -s "$topology_tmp" "$cleanup_staging_root"
expect_success \
  "trusted prepare заменяет /tmp symlink destinations реальными directories без изменения target" \
  "$cleanup_guard_binary" prepare deploy
[[ -d "$cleanup_target_root" && ! -L "$cleanup_target_root" && -d "$cleanup_staging_root" && ! -L "$cleanup_staging_root" && -f "$topology_tmp/marker" ]] || {
  echo "FAIL: trusted prepare не восстановил symlink topology или изменил /tmp target" >&2
  exit 1
}

printf 'hardlink target\n' >"$topology_tmp/hardlink-target"
ln "$topology_tmp/hardlink-target" "$cleanup_target_root/goose"
mkdir "$cleanup_target_root/staticcheck"
chmod 0777 "$cleanup_target_root"
expect_success \
  "trusted prepare удаляет hardlink/directory-as-file leaves и исправляет writable destination mode" \
  "$cleanup_guard_binary" prepare deploy
[[ ! -e "$cleanup_target_root/goose" && ! -e "$cleanup_target_root/staticcheck" && "$(stat -c '%h' "$topology_tmp/hardlink-target")" == 1 && "$(stat -c '%a' "$cleanup_target_root")" == 755 ]] || {
  echo "FAIL: trusted prepare сохранил hostile leaf topology или writable mode" >&2
  exit 1
}

protected_artifact_names=(
  buf
  gofumpt
  goimports
  golangci-lint
  goose
  grpcurl
  mockgen
  oapi-codegen
  protoc-gen-go
  protoc-gen-go-grpc
  sqlc
  staticcheck
  yq
)
deploy_protected_artifact_names=("${protected_artifact_names[@]}" mattercodex-shell)
services_protected_artifact_names=("${protected_artifact_names[@]}" matter-codex-agent-runner mattercodex-init)

populate_protected_staging() {
  local profile="${1:-deploy}"
  local artifact_name
  for artifact_name in "${protected_artifact_names[@]}"; do
    printf 'trusted artifact %s\n' "$artifact_name" >"$cleanup_staging_root/$artifact_name"
    chmod 0755 "$cleanup_staging_root/$artifact_name"
  done
  case "$profile" in
    deploy)
      cat >"$cleanup_staging_root/mattercodex-shell" <<'EOF'
#!/bin/sh
[ "$1" = sh ] || exit 96
shift
exec /bin/sh "$@"
EOF
      chmod 0755 "$cleanup_staging_root/mattercodex-shell"
      ;;
    services)
      cat >"$cleanup_staging_root/matter-codex-agent-runner" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >"$MATTERCODEX_ENTRYPOINT_CAPTURE"
exit "${MATTERCODEX_ENTRYPOINT_EXIT_CODE:-0}"
EOF
      cp "$cleanup_guard_binary" "$cleanup_staging_root/mattercodex-init"
      chmod 0755 "$cleanup_staging_root/matter-codex-agent-runner" "$cleanup_staging_root/mattercodex-init"
      ;;
    *)
      echo "FAIL: неизвестный protected staging profile $profile" >&2
      exit 1
      ;;
  esac
}

mkdir -p "$cleanup_go_root/bin"
cat >"$cleanup_go_root/bin/go" <<EOF
#!/usr/bin/env bash
case "\$*" in
  "env GOVERSION") printf 'go1.26.5\\n' ;;
  "env GOTOOLCHAIN") printf 'local\\n' ;;
  "env GOROOT") printf '%s\\n' '$cleanup_go_root' ;;
  "tool compile -V=full") printf 'compile version go1.26.5\\n' ;;
  *) exit 2 ;;
esac
EOF
chmod 0755 "$cleanup_go_root/bin/go"
cp "$cleanup_go_root/bin/go" "$topology_tmp/trusted-go"
populate_protected_staging
cp "$cleanup_staging_root/goose" "$topology_tmp/expected-goose"
printf 'destination hardlink target\n' >"$topology_tmp/destination-hardlink"
ln "$topology_tmp/destination-hardlink" "$cleanup_target_root/goose"
ln -s "$topology_tmp/attacker-staticcheck" "$cleanup_target_root/staticcheck"
mkdir "$cleanup_target_root/yq"
expect_success \
  "trusted install заменяет hostile destination leaves и фиксирует source/destination digests" \
  "$cleanup_guard_binary" install deploy "$cleanup_go_root/bin/go"
[[ ! -e "$cleanup_staging_root" && "$(stat -c '%h' "$topology_tmp/destination-hardlink")" == 1 ]] || {
  echo "FAIL: trusted install сохранил staging или destination hardlink" >&2
  exit 1
}
for artifact_name in "${deploy_protected_artifact_names[@]}"; do
  artifact_stat="$(stat -c '%u:%g:%a:%h:%F' "$cleanup_target_root/$artifact_name")"
  [[ "$artifact_stat" == "$(id -u):$(id -g):555:1:regular file" ]] || {
    echo "FAIL: protected artifact имеет неверные owner/mode/link/type: $artifact_name $artifact_stat" >&2
    exit 1
  }
done
cmp "$topology_tmp/expected-goose" "$cleanup_target_root/goose" || {
  echo "FAIL: trusted install изменил digest committed source goose" >&2
  exit 1
}
default_command_capture="$topology_tmp/default-command-capture"
cp /bin/true "$topology_tmp/sh"
chmod 0755 "$topology_tmp/sh"
expect_success \
  "protected absolute deploy CMD запускает exact shell artifact без PATH lookup" \
  env PATH="$topology_tmp:/usr/local/bin:/usr/bin:/bin" MATTERCODEX_DEFAULT_COMMAND_CAPTURE="$default_command_capture" \
  "$cleanup_target_root/mattercodex-shell" sh -c 'printf "protected default command\n" >"$MATTERCODEX_DEFAULT_COMMAND_CAPTURE"'
[[ "$(cat "$default_command_capture")" == "protected default command" ]] || {
  echo "FAIL: protected absolute deploy CMD не выполнил default shell probe" >&2
  exit 1
}

expect_success "trusted prepare для services runner provenance" "$cleanup_guard_binary" prepare services
mkdir -p "$cleanup_go_root/bin"
cp "$topology_tmp/trusted-go" "$cleanup_go_root/bin/go"
chmod 0755 "$cleanup_go_root/bin/go"
populate_protected_staging services
expect_success \
  "trusted services install включает runner в тот же topology/digest commitment" \
  "$cleanup_guard_binary" install services "$cleanup_go_root/bin/go"
runner_stat="$(stat -c '%u:%g:%a:%h:%F' "$cleanup_target_root/matter-codex-agent-runner")"
[[ "$runner_stat" == "$(id -u):$(id -g):555:1:regular file" ]] || {
  echo "FAIL: runner не является отдельным root/current-owned regular non-hardlinked protected artifact" >&2
  exit 1
}
entrypoint_capture="$topology_tmp/services-entrypoint-capture"
expect_success \
  "effective protected services entrypoint достигает exact protected runner и сохраняет argv" \
  env MATTERCODEX_ENTRYPOINT_CAPTURE="$entrypoint_capture" \
  "$cleanup_target_root/mattercodex-init" entrypoint "$cleanup_target_root/matter-codex-agent-runner" alpha "two words"
[[ "$(cat "$entrypoint_capture")" == "alpha two words" ]] || {
  echo "FAIL: protected services entrypoint не достиг exact runner с ожидаемым argv" >&2
  exit 1
}
expect_status \
  "protected services entrypoint передаёт exit status exact runner" \
  23 \
  env MATTERCODEX_ENTRYPOINT_CAPTURE="$entrypoint_capture" MATTERCODEX_ENTRYPOINT_EXIT_CODE=23 \
  "$cleanup_target_root/mattercodex-init" entrypoint "$cleanup_target_root/matter-codex-agent-runner"

for source_topology in symlink hardlink directory writable extra; do
  expect_success "trusted prepare для negative source topology $source_topology" "$cleanup_guard_binary" prepare deploy
  populate_protected_staging
  case "$source_topology" in
    symlink)
      rm "$cleanup_staging_root/goose"
      ln -s "$topology_tmp/source-symlink" "$cleanup_staging_root/goose"
      expected_topology_error='not a regular non-symlink file'
      ;;
    hardlink)
      rm "$cleanup_staging_root/goose"
      printf 'source hardlink\n' >"$topology_tmp/source-hardlink"
      ln "$topology_tmp/source-hardlink" "$cleanup_staging_root/goose"
      expected_topology_error='has 2 hardlinks'
      ;;
    directory)
      rm "$cleanup_staging_root/goose"
      mkdir "$cleanup_staging_root/goose"
      expected_topology_error='not a regular non-symlink file'
      ;;
    writable)
      chmod 0777 "$cleanup_staging_root/goose"
      expected_topology_error='group/world-writable'
      ;;
    extra)
      printf 'extra\n' >"$cleanup_staging_root/uncommitted"
      expected_topology_error='protected staging has 15 artifacts, want 14'
      ;;
  esac
  expect_failure_matching \
    "trusted install отклоняет $source_topology topology committed source" \
    "$expected_topology_error" \
    "$cleanup_guard_binary" install deploy "$cleanup_go_root/bin/go"
done

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
for index in "${!agent_runner_paths[@]}"; do
  path="${agent_runner_paths[$index]}"
  verify_run="${verify_runs[$index]}"
  split_exact_instruction \
    "$logical_contract_continuations/$path" \
    "$runtime_copy" \
    'COPY --from=0 /usr/local/go/ \' \
    '/usr/local/go/'
  split_exact_instruction \
    "$logical_contract_continuations/$path" \
    "$verify_run" \
    "${verify_run_prefixes[$index]}" \
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
replace_exact_instruction "$runtime_check_missing/deploy/images/agent-runner/Dockerfile" "${verify_runs[1]}" ""
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

echo "PASS: Go toolchain contract проверяет protected topology/digests/runner, literal BuildKit/Kaniko USER, executable wrapper flow и hostile mutations"

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

expect_success "неизменённый GOTOOLCHAIN=local" "$guard" --root "$repo_root" --static-only

agent_runner_paths=(
  services/jobs/agent-runner/Dockerfile
  deploy/images/agent-runner/Dockerfile
)
agent_runner_names=(services deploy)

for index in "${!agent_runner_paths[@]}"; do
  path="${agent_runner_paths[$index]}"
  name="${agent_runner_names[$index]}"

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
done

ascii_indentation_local="$temp_root/ascii-indentation-local"
copy_fixture "$ascii_indentation_local"
for path in "${agent_runner_paths[@]}"; do
  printf '\n \tENV GOTOOLCHAIN="local"\n' >>"$ascii_indentation_local/$path"
done
expect_success \
  "ASCII-пробел и табуляция перед безопасным ENV поддерживаются в обоих agent-runner Dockerfile" \
  "$guard" --root "$ascii_indentation_local" --static-only

ascii_continuation_suffix="$temp_root/ascii-continuation-suffix"
copy_fixture "$ascii_continuation_suffix"
for path in "${agent_runner_paths[@]}"; do
  printf '\nENV PATH=/usr/local/go/bin\\ \t\n    GOTOOLCHAIN="local"\n' >>"$ascii_continuation_suffix/$path"
done
expect_success \
  "ASCII-пробел и табуляция после escape сохраняют Dockerfile continuation" \
  "$guard" --root "$ascii_continuation_suffix" --static-only

modern_quoted_local="$temp_root/modern-quoted-local"
copy_fixture "$modern_quoted_local"
for path in "${agent_runner_paths[@]}"; do
  printf '%s\n' '' 'ENV GOTOOLCHAIN="local"' >>"$modern_quoted_local/$path"
done
expect_success \
  "local в кавычках современного ENV поддерживается в обоих agent-runner Dockerfile" \
  "$guard" --root "$modern_quoted_local" --static-only

legacy_quoted_local="$temp_root/legacy-quoted-local"
copy_fixture "$legacy_quoted_local"
for path in "${agent_runner_paths[@]}"; do
  printf '%s\n' '' 'ENV GOTOOLCHAIN "local"' >>"$legacy_quoted_local/$path"
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
cat >>"$runtime_final_local/services/jobs/agent-runner/Dockerfile" <<'EOF'

ENV GOTOOLCHAIN=auto
ENV PATH=/usr/local/go/bin:/usr/local/bin \
    GOTOOLCHAIN="local"
EOF
cat >>"$runtime_final_local/deploy/images/agent-runner/Dockerfile" <<'EOF'

ENV GOTOOLCHAIN=auto
ENV PATH=/usr/local/go/bin:/usr/local/bin \
    GOTOOLCHAIN="local"
EOF
expect_success "последний GOTOOLCHAIN=local определяет эффективное значение" "$guard" --root "$runtime_final_local" --static-only

legacy_runtime_final_local="$temp_root/legacy-runtime-final-local"
copy_fixture "$legacy_runtime_final_local"
cat >>"$legacy_runtime_final_local/services/jobs/agent-runner/Dockerfile" <<'EOF'

ENV GOTOOLCHAIN auto
ENV GOTOOLCHAIN local
EOF
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
  "services/jobs/agent-runner/Dockerfile final runtime stage завершает GOTOOLCHAIN значением 'auto' вместо 'local'" \
  "$guard" --root "$services_three_slash_override" --static-only

deploy_three_slash_override="$temp_root/deploy-three-slash-override"
copy_fixture "$deploy_three_slash_override"
cat >>"$deploy_three_slash_override/deploy/images/agent-runner/Dockerfile" <<'EOF'

RUN true \\\
ENV GOTOOLCHAIN=auto
EOF
expect_failure_matching \
  "три завершающих backslash перед поздним GOTOOLCHAIN=auto в deploy Dockerfile" \
  "deploy/images/agent-runner/Dockerfile final runtime stage завершает GOTOOLCHAIN значением 'auto' вместо 'local'" \
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

echo "PASS: Go toolchain contract отклоняет старую версию, итоговый runtime-stage drift и govulncheck injection"

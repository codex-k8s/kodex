#!/usr/bin/env bash
# Подключение только объявляет общие предохранители двух локальных сценариев подготовки учётных записей.
# Они сохраняют прежние проверки, не выдают полномочий Keycloak и не читают секреты.

# Проверяет прежние контекст, путь состояния, операторское подтверждение и запрет prod.
# Читает переменные вызывающего скрипта и использует его fail, не меняя cwd/options/argv.
kodex_validate_identity_fixture_scope() {
  [[ -n "$expected_context" ]] || fail 'exact context is required'
  [[ "$state_directory" == /* && "$state_directory" != / && "$state_directory" != "$HOME" &&
    -d "$state_directory" && ! -L "$state_directory" ]] ||
    fail 'state directory must be an exact existing safe absolute path'
  [[ "${KODEX_E2E_CONFIRM_DISPOSABLE:-}" == I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION ]] ||
    fail 'disposable installation confirmation is required'
  [[ "${expected_context,,}" != *prod* && "${expected_context,,}" != *production* ]] ||
    fail 'production context is forbidden'
}

# Сверяет прежние stdout current-context, rollout и метки namespace через kubectl.
# Выполняет прежние чтения; блокировки, секреты и мутации остаются у вызывающих скриптов.
# Исходное правило current-context сравнивает stdout, не проверяя exit status отдельно.
kodex_verify_identity_fixture_target() {
  [[ "$(kubectl config current-context)" == "$expected_context" ]] ||
    fail 'current Kubernetes context mismatch'
  kubectl -n "$namespace" rollout status "deployment/$deployment" --timeout=300s >/dev/null ||
    fail 'Keycloak deployment is unavailable'
  kubectl get namespace "$namespace" -o json | jq -e '
  .metadata.labels["app.kubernetes.io/part-of"] == "kodex" and
  .metadata.labels["kodex.dev/local-profile"] == "hot-reload"
' >/dev/null || fail 'identity namespace is not an exact local profile'
}

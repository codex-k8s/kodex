#!/usr/bin/env bash
# Общие чтения идентичности кластера; подключение только объявляет функции.

# Вызывающая capture-функция заранее объявляет local cluster_uid/config_json/
# api_endpoint. Bash передаёт их динамическую область этому helper; он сохраняет
# прежние два kubectl read и endpoint jq, не читает CA и не выводит конфигурацию.
kodex_capture_cluster_metadata() {
  cluster_uid=$(kubectl get namespace kube-system -o jsonpath='{.metadata.uid}') ||
    fail 'Kubernetes cluster UID readback failed'
  [[ "$cluster_uid" =~ ^[A-Za-z0-9-]{16,128}$ ]] || fail 'Kubernetes cluster UID is invalid'
  config_json=$(kubectl config view --minify --raw -o json) ||
    fail 'Kubernetes cluster configuration readback failed'
  api_endpoint=$(jq -er '
    .clusters | select(length == 1) | .[0].cluster.server |
    select(type == "string" and test("^https://[^[:space:]]+$"))
  ' <<<"$config_json") || fail 'Kubernetes API endpoint is invalid'
}

# Заполняет прежние local marker_stat/marker_json вызывающей функции.
# Сохраняет sudo read, regular/nonlink, root0:0:600 и schema проверки. Точный
# путь marker, печать результата и сравнение с кластером остаются у callers.
kodex_read_disposable_cluster_marker() {
  if ! sudo -n test -f "$cluster_marker" || sudo -n test -L "$cluster_marker"; then
    fail 'disposable cluster marker is absent or unsafe'
  fi
  marker_stat=$(sudo -n stat -c '%u:%g:%a' -- "$cluster_marker") ||
    fail 'disposable cluster marker metadata readback failed'
  [[ "$marker_stat" == 0:0:600 ]] || fail 'disposable cluster marker ownership or mode is invalid'
  marker_json=$(sudo -n cat -- "$cluster_marker") ||
    fail 'disposable cluster marker readback failed'
  jq -e '
    .version == 1 and
    (.clusterUID | type == "string" and test("^[A-Za-z0-9-]{16,128}$")) and
    (.apiEndpoint | type == "string" and test("^https://[^[:space:]]+$")) and
    (.caSHA256 | type == "string" and test("^[a-f0-9]{64}$"))
  ' <<<"$marker_json" >/dev/null || fail 'disposable cluster marker is invalid'
}

#!/usr/bin/env bash
# Подключение только объявляет общие проверки входа двух release-рендереров.
# Вызывающий скрипт сохраняет проверки размеров массивов и порядок provider guards.

# Проверяет прежнюю форму /32 или /128 в api_cidrs; семантического разбора IP нет.
# Сохраняет присваивание глобальной cidr и прежний fail вызывающего скрипта.
kodex_validate_kubernetes_api_cidrs() {
  for cidr in "${api_cidrs[@]}"; do
    [[ "$cidr" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/32$ || "$cidr" =~ ^[0-9a-fA-F:]+/128$ ]] ||
      fail "Kubernetes API endpoint must be IPv4 /32 or IPv6 /128"
  done
}

# Проверяет прежнее десятичное выражение диапазона api_ports и точное наличие 443.
# Использует уже инициализированную has_service_port; глобальные port и результат
# сохраняются. Вызывающий скрипт задаёт false до предшествующих проверок CIDR.
kodex_validate_kubernetes_api_ports() {
  for port in "${api_ports[@]}"; do
    [[ "$port" =~ ^[0-9]+$ ]] && ((10#$port >= 1 && 10#$port <= 65535)) || fail "Kubernetes API port is invalid"
    [[ "$port" == 443 ]] && has_service_port=true
  done
  [[ "$has_service_port" == true ]] || fail "Kubernetes API Service port 443 is required"
}

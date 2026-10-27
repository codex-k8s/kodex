#!/usr/bin/env bash
# Подключение только объявляет общий нормализатор вывода DNS двух preflight путей.

# Читает строки stdin, удаляя прежние пробельные символы по краям и конечные точки.
# Сохраняет пропуск неверных IP, отбор семейства и лексикографическую сортировку
# уникальных канонических строк Python ipaddress. DNS не запрашивает; допустимость
# адресов и разрешённые списки по-прежнему проверяют вызывающие скрипты.
# Аргумент family и его прежние ошибки передаются Python; Bash family остаётся local.
canonical_dns_addresses() {
  local family=$1
  python3 -c '
import ipaddress
import sys

family = int(sys.argv[1])
result = set()
for line in sys.stdin:
    value = line.strip().rstrip(".")
    if not value:
        continue
    try:
        address = ipaddress.ip_address(value)
    except ValueError:
        continue
    if address.version == family:
        result.add(str(address))
print("\n".join(sorted(result)))
' "$family"
}

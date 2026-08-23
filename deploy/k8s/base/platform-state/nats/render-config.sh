#!/bin/sh
set -eu
umask 077

operator_jwt=$(cat /var/run/credentials/operator.jwt)
system_account=$(cat /var/run/credentials/system-account.public)
system_account_jwt=$(cat /var/run/credentials/system-account.jwt)
application_account=$(cat /var/run/credentials/account.public)
application_account_jwt=$(cat /var/run/credentials/account.jwt)

case "$operator_jwt:$system_account_jwt:$application_account_jwt" in
  (*[!A-Za-z0-9._:-]*|'') exit 20 ;;
esac
case "$system_account:$application_account" in
  (*[!A-Z0-9:]*|'') exit 21 ;;
esac
[ "${#system_account}" -eq 56 ]
[ "${#application_account}" -eq 56 ]

sed \
  -e "s|__NATS_OPERATOR_JWT__|$operator_jwt|" \
  -e "s|__NATS_SYSTEM_ACCOUNT__|$system_account|g" \
  -e "s|__NATS_SYSTEM_ACCOUNT_JWT__|$system_account_jwt|" \
  -e "s|__NATS_APPLICATION_ACCOUNT__|$application_account|" \
  -e "s|__NATS_APPLICATION_ACCOUNT_JWT__|$application_account_jwt|" \
  /var/run/config/nats.conf > /var/run/runtime/nats.conf

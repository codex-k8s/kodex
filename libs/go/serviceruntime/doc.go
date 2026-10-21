// Package serviceruntime provides local readiness snapshots, explicit worker
// cancel/join ownership and cooperative per-operation shutdown budgets. It does
// not create root contexts or decide service-specific readiness policy.
// Пакет serviceruntime предоставляет локальные снимки готовности, явный cancel/join
// workers и кооперативные бюджеты shutdown. Корневые контексты и сервисную
// политику готовности задаёт вызывающий код, а не библиотека.
package serviceruntime

// Package restoreagent coordinates the workload's restore admission fence,
// drain and signed ACK with the external controller using delivered material.
// It does not own durable restore phases, controller decisions or scheduling.
// Пакет restoreagent согласует ограждение допуска workload при восстановлении,
// дренирование и подписанный ACK с внешним контроллером через доставленный
// материал. Устойчивыми фазами, решениями контроллера и расписанием не владеет.
package restoreagent

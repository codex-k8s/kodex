package restore

import _ "embed"

// applyFenceSQL embeds the database-owned fence application call with exact cluster ID, epoch, phase, evidence digest and optional safe-window time; declaring it does not change restore state.
// applyFenceSQL встраивает принадлежащий БД вызов применения ограждения с точными ID кластера, эпохой, фазой, дайджестом доказательства и необязательным временем безопасного окна; объявление не меняет состояние восстановления.
//
//go:embed sql/restore_fence__apply.sql
var applyFenceSQL string

// fenceReadinessSQL embeds existence readback for the exact cluster/epoch/phase/evidence fence and null-safe safe-window timestamp equality, not a general indication that runtime work is allowed.
// fenceReadinessSQL встраивает чтение существования ограждения с точными кластером, эпохой, фазой, доказательством и null-safe равенством времени безопасного окна, а не общий признак разрешения runtime-работы.
//
//go:embed sql/restore_fence__readiness.sql
var fenceReadinessSQL string

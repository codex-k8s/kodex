# Завершение PWA при релизе

Задача #1235. Frontend получает preStop sleep 10 секунд для распространения
удаления endpoint до ingress и общий grace не меньше 45 секунд. Это не
доказательство единственной причины ранее наблюдавшегося HTML timeout.
Readiness, источники, образы, env, TLS и security history не меняются.

Профиль объявлен в базовом Deployment. Уже работающий hot-reload staging
обновляется отдельно, только репозиторной командой, после разрешения владельца:

```bash
node tools/release/frontend-drain-profile.mjs \
  --context staging --evidence /private/frontend-drain.jsonl \
  --confirm APPLY-STAGING-FRONTEND-DRAIN
```

API server и все kubelets должны поддерживать stable SleepAction (Kubernetes
1.34+). Проверяются ownership, завершённый rollout и отсутствие чужого preStop.
Приватный O_EXCL журнал синхронизируется до CAS PATCH; следуют bounded rollout
и точный readback spec и доступности. После неизвестного исхода нельзя повторять
PATCH вслепую. Частичные результаты не удаляются.

Обычный application release не меняет этот профиль. Откат приложения не должен
удалять профиль drain или затрагивать соседей. Первая миграция отделяется в
evidence от последующих одиночных/групповых релизов. Для приёмки нужны реальные
HTML/API-пробы с refresh, сохранение всех отказов и сравнение соседних spec.
Production требует отдельного согласования и проверки профиля.

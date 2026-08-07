# Jobs и workers

Worker исполняет attempt, но не становится владельцем aggregate. Task,
immutable input, grant, retry и terminal result принадлежат доменному сервису.

- [agent-runner](agent-runner/README.md) исполняет один claimed Turn и signed
  runtime handoff без transport и orchestration authority.
- [automation-scheduler](automation-scheduler/README.md) ограниченно будит
  server-owned schedule lifecycle через защищённый control-plane path.
- [legacy-data-migration](legacy-data-migration/README.md) сверяет и закрывает
  one-shot перенос legacy bot-service state без compatibility facade.

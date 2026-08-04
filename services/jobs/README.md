# Jobs и workers

Worker исполняет attempt, но не становится владельцем aggregate. Task,
immutable input, grant, retry и terminal result принадлежат доменному сервису.

- [agent-runner](agent-runner/README.md) исполняет один claimed Turn и signed
  runtime handoff без transport и orchestration authority.

# Jobs и workers

Worker исполняет attempt, но не становится владельцем aggregate. Task,
immutable input, grant, retry и terminal result принадлежат доменному сервису.

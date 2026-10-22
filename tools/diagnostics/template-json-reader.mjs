import { execFileSync } from "node:child_process";

// Общий транспорт чтения двух metadata watchers: только создание замыкания,
// без запуска процесса до read. Caller задаёт проверенные base/env/deadline,
// а собственные UID/owner/run checks выполняет после чтения. Инъекция read
// сохраняет прежний путь оснастки: её результат не JSON-decode, ошибка не
// подавляется. execute заменяется только герметичной unit-оснасткой.
export function createTemplateJSONReader(
  base,
  env,
  deadline,
  runtime = {},
  execute = execFileSync,
) {
  return (args, template) => {
    if (Date.now() >= deadline || runtime.signal?.aborted) return null;
    if (runtime.read) return runtime.read(args, template);
    try {
      return JSON.parse(
        execute(
          "kubectl",
          [...base, ...args, "-o", `go-template=${template}`],
          {
            env,
            encoding: "utf8",
            stdio: ["ignore", "pipe", "ignore"],
            timeout: Math.min(6000, Math.max(1, deadline - Date.now())),
            killSignal: "SIGKILL",
            maxBuffer: 1 << 20,
          },
        ),
      );
    } catch {
      return null;
    }
  };
}

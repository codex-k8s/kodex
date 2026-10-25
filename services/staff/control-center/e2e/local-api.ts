import { randomUUID } from "node:crypto";
import type { Page } from "@playwright/test";

// Выполняет одну локальную E2E mutation с новым ключом, cookie CSRF и прежним
// If-Match. Callback самодостаточен для page.evaluate; неизвестный результат
// передаётся вызывающему без автоматической повторной записи.
export async function mutateLocalApi<T>(
  page: Page,
  path: string,
  body?: unknown,
  version?: number,
  method: "POST" | "PUT" = "POST",
): Promise<T> {
  const result = await page.evaluate(
    async (input) => {
      const prefix = `${encodeURIComponent("__Host-kodex-csrf")}=`;
      const csrf = document.cookie
        .split(";")
        .map((item) => item.trim())
        .find((item) => item.startsWith(prefix))
        ?.slice(prefix.length);
      if (!csrf)
        return { status: 0, code: "CSRF_UNAVAILABLE", body: undefined };
      const headers: Record<string, string> = {
        Accept: "application/json",
        "Content-Type": "application/json",
        "Idempotency-Key": input.key,
        "X-CSRF-Token": decodeURIComponent(csrf),
      };
      if (input.version !== undefined)
        headers["If-Match"] = `"${String(input.version)}"`;
      const response = await fetch(input.path, {
        method: input.method,
        headers,
        body: input.body === undefined ? undefined : JSON.stringify(input.body),
      });
      const decoded = (await response.json()) as Record<string, unknown>;
      return {
        status: response.status,
        code: typeof decoded.code === "string" ? decoded.code : "UNKNOWN",
        body: response.ok ? decoded : undefined,
      };
    },
    { path, body, version, method, key: randomUUID() },
  );
  if (result.status < 200 || result.status >= 300 || !result.body)
    throw new Error(
      `API mutation failed: ${String(result.status)} ${result.code}`,
    );
  return result.body as T;
}

import { createSSRApp } from "vue";
import { renderToString } from "vue/server-renderer";
import { createI18n } from "vue-i18n";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { SessionPhase } from "@/features/session/store";
import type { AppProblem } from "@/shared/api/problem";
import AuthGate from "./AuthGate.vue";

const session = vi.hoisted(() => ({
  phase: "checking" as SessionPhase,
  problem: undefined as AppProblem | undefined,
  loginFailed: false,
  beginLogin: vi.fn(),
  probe: vi.fn(),
}));
vi.mock("@/features/session/store", () => ({
  useSessionStore: () => session,
}));

async function renderGate(): Promise<string> {
  const app = createSSRApp(AuthGate);
  app.use(
    createI18n({
      legacy: false,
      locale: "ru",
      messages: {
        ru: {
          auth: {
            title: "Вход в Kodex",
            description: "Войдите через провайдера организации.",
            checking: "Проверяем защищённую сессию…",
            signIn: "Войти",
          },
          common: {
            error: "Ошибка",
            forbidden: "Недостаточно прав",
            retry: "Повторить",
          },
          errors: { default: "Не удалось проверить сессию" },
        },
      },
    }),
  );
  return await renderToString(app);
}

beforeEach(() => {
  vi.clearAllMocks();
  session.phase = "checking";
  session.problem = undefined;
  session.loginFailed = false;
});

describe("AuthGate: начальная проверка сессии", () => {
  it("до ответа показывает нейтральный статус, а не ложный экран входа", async () => {
    const html = await renderGate();
    expect(html).toContain('role="status"');
    expect(html).toContain("Проверяем защищённую сессию…");
    expect(html).not.toContain("Вход в Kodex");
    expect(html).not.toContain("Войдите через");
    expect(html).not.toContain("<button");
    expect(html).not.toContain('role="alert"');
    expect(session.beginLogin).not.toHaveBeenCalled();
    expect(session.probe).not.toHaveBeenCalled();
  });

  it("после авторитетного 401 сохраняет экран и кнопку входа", async () => {
    session.phase = "unauthenticated";
    const html = await renderGate();
    expect(html).toContain("Вход в Kodex");
    expect(html).toContain("Войдите через");
    expect(html).toContain("<button");
    expect(html).toContain("Войти</button>");
    expect(html).not.toContain("Проверяем защищённую сессию…");
  });

  it.each(["error", "forbidden"] as const)(
    "не заменяет завершившийся %s бесконечной загрузкой",
    async (phase) => {
      session.phase = phase;
      const html = await renderGate();
      expect(html).toContain('role="alert"');
      expect(html).toContain("Не удалось проверить сессию");
      expect(html).not.toContain('role="status"');
      expect(html).not.toContain("Войти</button>");
    },
  );
});

import { readFileSync } from "node:fs";
import {
  createRenderer,
  createSSRApp,
  defineComponent,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { renderToString } from "vue/server-renderer";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/app/i18n";
import { AppProblem } from "@/shared/api/problem";
import AuthCallbackView from "./AuthCallbackView.vue";
const session = vi.hoisted(() => ({
  problem: undefined as AppProblem | undefined,
  completeLogin: vi.fn(),
  beginLogin: vi.fn(),
}));
const platform = vi.hoisted(() => ({
  loadBootstrap: vi.fn(),
  bootstrap: { onboardingComplete: true },
}));
const router = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("@/features/session/store", () => ({ useSessionStore: () => session }));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => platform,
}));
vi.mock("vue-router", () => ({ useRouter: () => router }));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
interface State {
  problem: Ref<AppProblem | undefined>;
  restartLogin(): Promise<void>;
}
function mount() {
  let state: State | undefined;
  const renderer = createRenderer<object, object>({
    patchProp() {},
    insert() {},
    remove() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
  });
  const source = AuthCallbackView as unknown as {
    setup(props: unknown, context: SetupContext): State;
  };
  const app = renderer
    .createApp(
      defineComponent({
        setup(props, context) {
          state = source.setup(props, context);
          return () => null;
        },
      }),
    )
    .use(i18n);
  app.provide(ssrContextKey, {});
  app.mount({});
  if (!state) throw new Error("Callback setup is unavailable");
  const captured = state;
  return {
    state: captured,
    render: async () =>
      renderToString(
        createSSRApp({
          ...AuthCallbackView,
          setup: () => captured,
        }).use(i18n),
      ),
    unmount: () => app.unmount(),
  };
}
beforeEach(() => {
  vi.clearAllMocks();
  i18n.global.locale.value = "ru";
  session.problem = undefined;
  session.beginLogin.mockResolvedValue(undefined);
  platform.loadBootstrap.mockResolvedValue(undefined);
});
describe("AuthCallbackView: новый вход после закрытого отказа", () => {
  it.each(["ru", "en"] as const)(
    "401 показывает явный вход без auto replay (%s)",
    async (locale) => {
      i18n.global.locale.value = locale;
      session.completeLogin.mockRejectedValue(
        new AppProblem({
          status: 401,
          code: "UNAUTHENTICATED",
          kind: "unauthorized",
          retryable: false,
        }),
      );
      const fixture = mount();
      try {
        await vi.waitFor(() =>
          expect(fixture.state.problem.value?.kind).toBe("unauthorized"),
        );
        const html = await fixture.render(),
          expected = locale === "ru" ? "Войти" : "Sign in";
        expect(html).toMatch(
          new RegExp(`<button[^>]*>\\s*${expected}\\s*</button>`),
        );
        expect(html).not.toContain(i18n.global.t("auth.callback"));
        expect(html).toContain(i18n.global.t("auth.failed"));
        expect(html).toContain(i18n.global.t("auth.callbackSignInRequired"));
        expect(session.completeLogin).toHaveBeenCalledOnce();
        expect(session.beginLogin).not.toHaveBeenCalled();
        expect(router.replace).not.toHaveBeenCalled();
        const source = readFileSync(
          new URL("./AuthCallbackView.vue", import.meta.url),
          "utf8",
        );
        expect(source).toMatch(
          /<button[^]*?@click="restartLogin"[^]*?auth.signIn/,
        );
        await fixture.state.restartLogin();
        expect(session.beginLogin).toHaveBeenCalledOnce();
        expect(session.completeLogin).toHaveBeenCalledOnce();
      } finally {
        fixture.unmount();
      }
    },
  );
  it.each([
    {
      kind: "unavailable" as const,
      status: 503,
      code: "UNAVAILABLE",
      retryable: true,
    },
    {
      kind: "unknown" as const,
      status: 400,
      code: "UNKNOWN",
      retryable: false,
    },
    {
      kind: "forbidden" as const,
      status: 403,
      code: "FORBIDDEN",
      retryable: false,
    },
  ])("$kind не заменяется новым входом", async (problem) => {
    session.completeLogin.mockRejectedValue(new AppProblem(problem));
    const fixture = mount();
    try {
      await vi.waitFor(() =>
        expect(fixture.state.problem.value?.kind).toBe(problem.kind),
      );
      const html = await fixture.render();
      expect(html).toContain(i18n.global.t("auth.failed"));
      expect(html).not.toContain(i18n.global.t("auth.callbackSignInRequired"));
      expect(html).not.toMatch(/<button[^>]*>\s*Войти\s*<\/button>/);
      if (problem.retryable) expect(html).toContain("Повторить");
      else expect(html).not.toContain("<button");
      expect(session.beginLogin).not.toHaveBeenCalled();
      expect(session.completeLogin).toHaveBeenCalledOnce();
    } finally {
      fixture.unmount();
    }
  });
  it("успешный callback сохраняет bootstrap и canonical return path", async () => {
    session.completeLogin.mockResolvedValue({ kind: "login" });
    const fixture = mount();
    try {
      await vi.waitFor(() => expect(router.replace).toHaveBeenCalledWith("/"));
      expect(platform.loadBootstrap).toHaveBeenCalledOnce();
      expect(session.beginLogin).not.toHaveBeenCalled();
    } finally {
      fixture.unmount();
    }
  });
});

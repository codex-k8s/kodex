import { createPinia } from "pinia";
import { createApp } from "vue";

import App from "@/App.vue";
import { i18n } from "@/app/i18n";
import { router } from "@/app/router";
import { installSessionGuard } from "@/app/router/session-guard";
import "@/app/styles/base.css";
import { useSessionStore } from "@/features/session/store";
import { useRealtimeStore } from "@/features/realtime/store";
import { usePlatformStore } from "@/features/platform/store";
import { configureApiClient } from "@/shared/api/client";
import { setUnauthorizedHandler } from "@/shared/api/problem";
import { loadRuntimeConfig } from "@/shared/config/runtime";

window.addEventListener("vite:preloadError", () => {
  document.documentElement.dataset.kodexPreload = "failed";
  window.dispatchEvent(new Event("kodex:preload-error"));
});

async function bootstrap(): Promise<void> {
  await loadRuntimeConfig();
  configureApiClient();
  const app = createApp(App);
  const pinia = createPinia();
  app.use(pinia);
  app.use(i18n);
  const session = useSessionStore(pinia);
  installSessionGuard(router, session);
  setUnauthorizedHandler(() => {
    session.invalidate();
    useRealtimeStore(pinia).closeAll();
    usePlatformStore(pinia).clearOwnerState();
  });
  app.use(router);
  app.mount("#app");
  if ("serviceWorker" in navigator && import.meta.env.PROD) {
    void navigator.serviceWorker
      .register("/sw.js", { scope: "/", updateViaCache: "none" })
      .then(() => undefined)
      .catch(() => {
        console.error("Service worker registration failed");
      });
  }
}

bootstrap().catch((error: unknown) => {
  document.documentElement.lang = i18n.global.locale.value;
  document.documentElement.dataset.kodexBootstrap = "failed";
  document.body.textContent = i18n.global.t("errors.default");
  console.error("Control Center bootstrap failed", error);
});

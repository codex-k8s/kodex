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

const preloadRecoveryKey = "kodex.preload-recovery";

window.addEventListener("vite:preloadError", (event) => {
  event.preventDefault();
  if (sessionStorage.getItem(preloadRecoveryKey) === "pending") {
    console.error("Dynamic module recovery failed after page reload");
    return;
  }
  sessionStorage.setItem(preloadRecoveryKey, "pending");
  globalThis.setTimeout(() => window.location.reload(), 300);
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
  app.use(router);
  await router.isReady();
  setUnauthorizedHandler(() => {
    session.invalidate();
    useRealtimeStore(pinia).closeAll();
    usePlatformStore(pinia).clearOwnerState();
  });
  app.mount("#app");
  sessionStorage.removeItem(preloadRecoveryKey);
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
  document.body.textContent = i18n.global.t("errors.default");
  console.error("Control Center bootstrap failed", error);
});

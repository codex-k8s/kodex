import { createPinia } from "pinia";
import { createApp, watch } from "vue";

import App from "@/App.vue";
import { i18n } from "@/app/i18n";
import { resetPrivateRuntime } from "@/app/private-runtime";
import { router } from "@/app/router";
import "@/app/styles/base.css";
import { useSessionStore } from "@/features/session/store";
import { usePwaUpdateStore } from "@/features/pwa-update/store";
import { configureApiClient } from "@/shared/api/client";
import { setUnauthorizedHandler } from "@/shared/api/problem";
import { loadRuntimeConfig } from "@/shared/config/runtime";
import { clearMutationIntents } from "@/shared/lib/identity";

async function bootstrap(): Promise<void> {
  await loadRuntimeConfig();
  configureApiClient();
  const app = createApp(App);
  const pinia = createPinia();
  app.use(pinia);
  app.use(i18n);
  app.use(router);
  const session = useSessionStore(pinia);
  const pwaUpdate = usePwaUpdateStore(pinia);
  setUnauthorizedHandler(() => {
    session.invalidate();
    clearMutationIntents();
    resetPrivateRuntime(pinia);
  });
  watch(
    () => session.phase,
    (phase) => {
      if (phase !== "unauthenticated") return;
      clearMutationIntents();
      resetPrivateRuntime(pinia);
    },
    { flush: "sync" },
  );
  app.mount("#app");
  if ("serviceWorker" in navigator && import.meta.env.PROD) {
    void navigator.serviceWorker
      .register("/sw.js", { scope: "/", updateViaCache: "none" })
      .then((registration) => {
        const notify = () => {
          if (!registration.waiting) return;
          pwaUpdate.updateFound(registration);
        };
        notify();
        registration.addEventListener("updatefound", () => {
          const worker = registration.installing;
          worker?.addEventListener("error", () => {
            console.error("Service worker update failed");
            pwaUpdate.registrationFailed();
          });
          worker?.addEventListener("statechange", () => {
            if (
              worker.state === "installed" &&
              navigator.serviceWorker.controller
            )
              notify();
            else if (worker.state === "redundant") {
              console.error("Service worker update became redundant");
              pwaUpdate.registrationFailed();
            }
          });
        });
      })
      .catch(() => {
        console.error("Service worker registration failed");
        pwaUpdate.registrationFailed();
      });
  }
}

bootstrap().catch((error: unknown) => {
  document.body.textContent = "Control Center bootstrap failed.";
  console.error("Control Center bootstrap failed", error);
});

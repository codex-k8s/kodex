import { createPinia } from "pinia";
import { createApp } from "vue";

import App from "@/App.vue";
import { i18n } from "@/app/i18n";
import { router } from "@/app/router";
import "@/app/styles/base.css";
import { configureApiClient } from "@/shared/api/client";
import { loadRuntimeConfig } from "@/shared/config/runtime";

async function bootstrap(): Promise<void> {
  await loadRuntimeConfig();
  configureApiClient();
  const app = createApp(App);
  app.use(createPinia());
  app.use(i18n);
  app.use(router);
  app.mount("#app");
  if ("serviceWorker" in navigator && import.meta.env.PROD) {
    await navigator.serviceWorker.register("/sw.js", { scope: "/" });
  }
}

bootstrap().catch((error: unknown) => {
  document.body.textContent = "Control Center bootstrap failed.";
  console.error("Control Center bootstrap failed", error);
});

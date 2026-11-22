import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { afterEach, expect, it } from "vitest";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import { kubernetesIdentitySetupMessages } from "../kubernetes-identity-setup-messages";
import KubernetesIdentitySetupPanel from "./KubernetesIdentitySetupPanel.vue";

afterEach(() => resetOwnerRequests());
it.each(["INITIATOR_REQUIRED", "PROJECT_SHARED"] as const)(
  "Kubernetes setup uses actual policy %s in the rendered title",
  async (policy) => {
    const app = createSSRApp({
      render: () =>
        h(KubernetesIdentitySetupPanel, {
          context: {
            connectionRef: "int_fixture001",
            connectionVersion: 7,
            expectedVersion: policy === "PROJECT_SHARED" ? 7 : 0,
            policy,
            available: false,
            enabled: false,
          },
        }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale: "ru",
        messages: {
          ru: { kubernetesIdentitySetup: kubernetesIdentitySetupMessages.ru },
        },
      }),
    );
    const html = await renderToString(app);
    expect(html).toContain(
      policy === "PROJECT_SHARED"
        ? "Сервисное подключение Kubernetes"
        : "Личное подключение Kubernetes",
    );
    expect(html).not.toContain(
      policy === "PROJECT_SHARED"
        ? "Личное подключение Kubernetes"
        : "Сервисное подключение Kubernetes",
    );
  },
);

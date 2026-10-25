import { renderToString } from "@vue/server-renderer";
import { createSSRApp, ref } from "vue";
import { createI18n } from "vue-i18n";
import { describe, expect, it, vi } from "vitest";
import type { SystemAssistantIntegrationGrantCandidate } from "@/shared/api/generated/openapi/types.gen";
import Fields from "./components/AssistantIntegrationGrantPolicyFields.vue";
import { useIntegrationGrantPlanPolicy } from "./use-integration-grant-plan-policy";

// Синтетический WRITE-кандидат с двумя разрешёнными политиками и 17 путями:
// выбранные 16 должны оставаться доступными для снятия последнего флажка.
function writeCandidate(): SystemAssistantIntegrationGrantCandidate {
  return {
    capability: {
      key: "fixture.write",
      name: "Запись",
      description: "Синтетическая запись",
      risk: "WRITE",
      approvalRequired: true,
      operation: "CREATE",
      resourceKind: "GITHUB_REPOSITORY",
      inputFields: [],
      approvalPolicy: "HUMAN_EACH_EFFECT",
      allowedApprovalPolicies: ["HUMAN_EACH_EFFECT", "HUMAN_SCOPED"],
      inputSchema: JSON.stringify({
        type: "object",
        properties: Object.fromEntries(
          Array.from({ length: 17 }, (_, index) => [
            `field-${String(index)}`,
            { type: "string" },
          ]),
        ),
      }),
    },
    grantable: true,
    reason: "READY",
    currentGrantVersion: 0,
    currentGrantEnabled: false,
    currentApprovalPolicy: "HUMAN_SCOPED",
    currentApprovalScopePaths: [],
  };
}

describe("Общие поля политики SYSTEM и PROJECT grant plan", () => {
  it("не подставляет default, реактивно сужает policy и отделяет состояние соседних форм", () => {
    const candidate = ref<SystemAssistantIntegrationGrantCandidate>();
    const parameters = ref<Record<string, unknown>>({
      approvalScopePaths: ["/field-0"],
    });
    const state = useIntegrationGrantPlanPolicy(
      candidate,
      (key) => parameters.value[key],
      vi.fn(),
    );
    const neighbor = useIntegrationGrantPlanPolicy(
      ref(),
      () => undefined,
      vi.fn(),
    );
    expect(state.policy.value).toBeUndefined();
    candidate.value = writeCandidate();
    expect(state.policy.value).toBeUndefined();
    parameters.value.approvalPolicy = "HUMAN_SCOPED";
    expect(state.policy.value).toBe("HUMAN_SCOPED");
    expect(neighbor.allowedPaths.value).toEqual([]);
    candidate.value.capability.allowedApprovalPolicies = ["HUMAN_EACH_EFFECT"];
    expect(state.policy.value).toBeUndefined();
    parameters.value.approvalScopePaths = ["/field-0", 1];
    expect(state.paths.value).toEqual([]);
  });

  it("сбрасывает paths до смены policy, отклоняет неизвестные значения и сортирует выбор без дублей", () => {
    const changed = vi.fn();
    const state = useIntegrationGrantPlanPolicy(
      ref(writeCandidate()),
      (key) =>
        key === "approvalScopePaths"
          ? ["/field-2", "/field-1"]
          : "HUMAN_SCOPED",
      changed,
    );
    state.changePolicy("NONE");
    state.togglePath("/foreign", true);
    expect(changed).not.toHaveBeenCalled();
    state.changePolicy("HUMAN_EACH_EFFECT");
    expect(changed.mock.calls).toEqual([
      ["approvalScopePaths", []],
      ["approvalPolicy", "HUMAN_EACH_EFFECT"],
    ]);
    changed.mockClear();
    state.togglePath("/field-1", true);
    state.togglePath("/field-2", false);
    expect(changed.mock.calls).toEqual([
      ["approvalScopePaths", ["/field-1", "/field-2"]],
      ["approvalScopePaths", ["/field-1"]],
    ]);
  });

  it.each([false, true])(
    "сохраняет disabled=%s, лимит 16 и readonly APPLIED без лишней обёртки",
    async (disabled) => {
      const candidate = writeCandidate();
      const state = useIntegrationGrantPlanPolicy(
        ref(candidate),
        () => undefined,
        vi.fn(),
      );
      const props = {
        id: "grant-fixture",
        candidate,
        disabled,
        enabled: true,
        fields: {
          policy: "HUMAN_SCOPED" as const,
          paths: state.allowedPaths.value.slice(0, 16),
          allowedPaths: state.allowedPaths.value,
          allowedPolicies: state.allowedPolicies.value,
        },
        connectionName: "Синтетическое соединение",
        recipientScope: "PROJECT" as const,
      };
      // SSR проверяет состав и атрибуты controls; это не browser/layout acceptance.
      async function html(appliedGrantRef?: string): Promise<string> {
        const app = createSSRApp(Fields, { ...props, appliedGrantRef });
        app.use(
          createI18n({
            legacy: false,
            locale: "ru",
            missingWarn: false,
            fallbackWarn: false,
          }),
        );
        return renderToString(app);
      }
      const editable = await html();
      const checkboxes = editable.match(/<input\b[^>]*>/g) ?? [];
      expect(checkboxes).toHaveLength(18);
      expect(
        checkboxes.filter((input) => input.includes(" disabled")),
      ).toHaveLength(disabled ? 18 : 1);
      expect(editable).toContain('id="grant-fixture-policy"');
      expect(editable).not.toContain("<div");
      const applied = await html("grant_applied_fixture");
      expect(applied).toContain('role="status"');
      expect(applied).not.toMatch(/<(input|select|fieldset)\b/);
    },
  );
});

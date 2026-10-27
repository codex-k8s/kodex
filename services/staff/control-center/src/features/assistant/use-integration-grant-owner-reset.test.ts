import { defineComponent, h, ref } from "vue";
import { expect, it, vi } from "vitest";
import { createLifecycleTestRenderer } from "@/test-utils/lifecycle-renderer";
import type {
  IntegrationConnection,
  SystemAssistantIntegrationGrantCandidate,
} from "@/shared/api/generated/openapi/types.gen";
import { useIntegrationGrantOwnerReset } from "./use-integration-grant-plan-draft";

function setup() {
  const owner = new AbortController();
  const connection = ref<IntegrationConnection>({
    ref: "synthetic",
  } as IntegrationConnection);
  const candidate = ref<SystemAssistantIntegrationGrantCandidate>(
    {} as SystemAssistantIntegrationGrantCandidate,
  );
  const loading = ref(true);
  const invalidate = vi.fn();
  const app = createLifecycleTestRenderer().createApp(
    defineComponent({
      setup() {
        useIntegrationGrantOwnerReset(
          owner.signal,
          connection,
          candidate,
          loading,
          invalidate,
        );
        return () => h("test");
      },
    }),
  );
  app.mount({});
  return { owner, connection, candidate, loading, invalidate, app };
}

it("отмена owner синхронно закрывает readback и один раз отзывает valid", () => {
  const state = setup();
  state.owner.abort();
  expect(state.connection.value).toBeUndefined();
  expect(state.candidate.value).toBeUndefined();
  expect(state.loading.value).toBe(false);
  expect(state.invalidate).toHaveBeenCalledOnce();
  state.app.unmount();
});

it("после размонтирования отмена owner не меняет состояние формы", () => {
  const state = setup();
  state.app.unmount();
  state.owner.abort();
  expect(state.connection.value.ref).toBe("synthetic");
  expect(state.candidate.value).toBeDefined();
  expect(state.loading.value).toBe(true);
  expect(state.invalidate).not.toHaveBeenCalled();
});

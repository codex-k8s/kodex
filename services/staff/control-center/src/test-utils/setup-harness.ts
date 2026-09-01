import { renderToString } from "@vue/server-renderer";
import {
  createSSRApp,
  defineComponent,
  h,
  type App,
  type Component,
  type SetupContext,
} from "vue";

type SetupState = Record<string, unknown>;
type SetupComponent = Component & {
  setup?: (props: Record<string, never>, context: SetupContext) => unknown;
};

export async function captureSetupState(
  component: SetupComponent,
  configure?: (app: App) => void,
): Promise<SetupState> {
  const originalSetup = component.setup;
  if (!originalSetup) throw new Error("Component setup is not defined");
  let captured: SetupState | undefined;
  const harness = defineComponent({
    setup(_props, context) {
      const result = originalSetup({}, context);
      if (result instanceof Promise) {
        throw new Error(
          "Async component setup is not supported by this harness",
        );
      }
      if (!result || typeof result !== "object") {
        throw new Error("Component setup state is not an object");
      }
      captured = result as SetupState;
      return () => h("setup-harness");
    },
  });
  const app = createSSRApp(harness);
  configure?.(app);
  await renderToString(app);
  if (!captured) throw new Error("Component setup state was not captured");
  return captured;
}

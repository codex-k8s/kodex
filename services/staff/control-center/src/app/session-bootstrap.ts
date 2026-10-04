import { watch } from "vue";
import type { useSessionStore } from "@/features/session/store";
import type { usePlatformStore } from "@/features/platform/store";

// Проверенный HTTP bootstrap устанавливается до дочерних экранов и realtime.
// После переноса session не хранит второй mutable источник bootstrap.
export function synchronizeSessionBootstrap(
  session: Pick<
    ReturnType<typeof useSessionStore>,
    "authenticatedBootstrap" | "takeAuthenticatedBootstrap"
  >,
  platform: Pick<
    ReturnType<typeof usePlatformStore>,
    "applyAuthenticatedBootstrap"
  >,
): ReturnType<typeof watch> {
  return watch(
    () => session.authenticatedBootstrap,
    (value) => {
      if (!value) return;
      const snapshot = session.takeAuthenticatedBootstrap();
      if (snapshot) platform.applyAuthenticatedBootstrap(snapshot);
    },
    { immediate: true, flush: "sync" },
  );
}

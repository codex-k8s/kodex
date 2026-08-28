import { onBeforeUnmount, onMounted, ref, type Ref } from "vue";

export function useBackgroundRefresh(
  refresh: () => Promise<void>,
  intervalMs = 30_000,
): {
  refreshing: Ref<boolean>;
  lastUpdatedAt: Ref<Date | undefined>;
  run: () => Promise<void>;
} {
  const refreshing = ref(false);
  const lastUpdatedAt = ref<Date>();
  let timer: ReturnType<typeof setInterval> | undefined;

  async function run(): Promise<void> {
    if (refreshing.value) return;
    refreshing.value = true;
    try {
      await refresh();
      lastUpdatedAt.value = new Date();
    } finally {
      refreshing.value = false;
    }
  }

  onMounted(() => {
    void run();
    timer = setInterval(() => void run(), intervalMs);
  });
  onBeforeUnmount(() => {
    if (timer) clearInterval(timer);
  });

  return { refreshing, lastUpdatedAt, run };
}

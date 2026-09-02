const retryDelaysMs = [200, 600, 1_500, 3_000] as const;

export function lazyPage<T>(load: () => Promise<T>): () => Promise<T> {
  return async () => {
    for (const delayMs of [0, ...retryDelaysMs]) {
      if (delayMs > 0) await delay(delayMs);
      try {
        return await load();
      } catch (error) {
        if (
          !isTransientModuleLoadError(error) ||
          delayMs === retryDelaysMs.at(-1)
        )
          throw error;
      }
    }
    throw new Error("unreachable route module retry state");
  };
}

function isTransientModuleLoadError(error: unknown): boolean {
  if (!(error instanceof TypeError)) return false;
  return /failed to fetch dynamically imported module|importing a module script failed|error loading dynamically imported module/i.test(
    error.message,
  );
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => globalThis.setTimeout(resolve, milliseconds));
}

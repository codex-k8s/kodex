export interface PlatformReloadOperation {
  run: () => Promise<void>;
}

export async function runBoundedPlatformReload(
  operations: readonly PlatformReloadOperation[],
  concurrency = 4,
): Promise<void> {
  if (!Number.isSafeInteger(concurrency) || concurrency < 1)
    throw new Error("Platform reload concurrency is invalid");
  let nextIndex = 0;
  let firstError: Error | undefined;
  const worker = async (): Promise<void> => {
    while (nextIndex < operations.length) {
      const operation = operations[nextIndex];
      nextIndex += 1;
      if (!operation) return;
      try {
        await operation.run();
      } catch (error) {
        firstError ??=
          error instanceof Error ? error : new Error("Platform reload failed");
      }
    }
  };
  await Promise.all(
    Array.from({ length: Math.min(concurrency, operations.length) }, async () =>
      worker(),
    ),
  );
  if (firstError !== undefined) throw firstError;
}

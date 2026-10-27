// Создаёт независимый pending Promise и синхронно захватывает его native callbacks.
// Завершение, ошибки и cleanup запускает caller; фабрика не ставит таймеров,
// не регистрирует глобальные handlers и не делит состояние между вызовами.
export function createDeferred<T = void>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

// Регистрирует завершение синтетического запроса при очистке теста и возвращает его callbacks.
export function createDeferredWithCleanup<T>(
  cleanup: Array<() => void>,
  value: T,
) {
  const { promise, resolve, reject } = createDeferred<T>();
  cleanup.push(() => resolve(value));
  return { promise, resolve, reject };
}

// createSearchScheduler откладывает поиск на 250 мс и отменяет предыдущий вызов.
// Вызывающий управляет подавлением запросов и отменяет таймер при размонтировании.
export function createSearchScheduler(search: (query: string) => void) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return {
    schedule(query: string) {
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => search(query.trim()), 250);
    },
    cancel() {
      if (timer) clearTimeout(timer);
    },
  };
}

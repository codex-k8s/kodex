import type { SyntheticFetchEvent } from "./synthetic-abort-observer";

// Связывает ровно одно поколение fetch с ровно одним network request.
// Завершённые поколения сохраняются: поздний abort не присваивается следующему URL.
export class SyntheticFetchCorrelator<T extends object> {
  private readonly events = new Map<
    string,
    { url: string; aborted: boolean; request?: T }
  >();
  private readonly requests = new Map<T, { url: string; id?: string }>();

  observe(event: SyntheticFetchEvent): void {
    const existing = this.events.get(event.id);
    if (existing) {
      if (existing.url === event.url && event.phase === "abort")
        existing.aborted = true;
    } else if (event.phase === "start") {
      this.events.set(event.id, { url: event.url, aborted: false });
    }
    this.match(event.url);
  }

  request(request: T, url: string): void {
    this.requests.set(request, { url });
    this.match(url);
  }

  cancelled(request: T): boolean {
    const id = this.requests.get(request)?.id;
    return id !== undefined && this.events.get(id)?.aborted === true;
  }

  describe(request: T): string {
    const matched = this.requests.get(request);
    const entries = [...this.events.values()].filter(
      (event) => event.url === matched?.url,
    );
    return `requests=${String([...this.requests.values()].filter((entry) => entry.url === matched?.url).length)}; receipts=${String(entries.length)}; bound=${String(!!matched?.id)}; aborted=${String(entries.filter((entry) => entry.aborted).length)}`;
  }

  private match(url: string): void {
    const events = [...this.events.entries()].filter(
      ([, event]) => event.url === url && !event.request,
    );
    const requests = [...this.requests.entries()].filter(
      ([, request]) => request.url === url && !request.id,
    );
    if (events.length !== 1 || requests.length !== 1) return;
    const event = events[0];
    const request = requests[0];
    if (!event || !request) return;
    event[1].request = request[0];
    request[1].id = event[0];
  }
}

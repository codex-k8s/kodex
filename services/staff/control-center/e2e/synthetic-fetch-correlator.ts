import type { SyntheticFetchEvent } from "./synthetic-abort-observer";

const identityPattern =
  /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}:[1-9][0-9]{0,6}$/;

// Identity приходит из одного fixture fetch и заголовка именно его network request.
// Совпадение URL/порядка или число одновременно завершившихся запросов не используются.
export class SyntheticFetchCorrelator<T extends object> {
  private readonly navigationIdentities = new Set<string>();
  private readonly terminalRequests = new WeakSet<T>();
  private readonly events = new Map<
    string,
    {
      url: string;
      started: boolean;
      aborted: boolean;
      bodyCompleted: boolean;
      bodyErrored: boolean;
      rejected: boolean;
      invalid: boolean;
    }
  >();
  private readonly requests = new Map<
    T,
    { url: string; id: string | undefined }
  >();
  private readonly identities = new Map<string, Set<T>>();

  observe(event: SyntheticFetchEvent): void {
    if (!identityPattern.test(event.id)) return;
    const existing = this.events.get(event.id);
    if (existing) {
      if (
        existing.url !== event.url ||
        (existing.started && event.phase === "start")
      )
        existing.invalid = true;
      if (event.phase === "abort") existing.aborted = true;
      else if (event.phase === "body") existing.bodyCompleted = true;
      else if (event.phase === "body-error") existing.bodyErrored = true;
      else if (event.phase === "reject") existing.rejected = true;
      else if (event.phase === "start") existing.started = true;
    } else {
      this.events.set(event.id, {
        url: event.url,
        started: event.phase === "start",
        aborted: event.phase === "abort",
        bodyCompleted: event.phase === "body",
        bodyErrored: event.phase === "body-error",
        rejected: event.phase === "reject",
        invalid: false,
      });
    }
  }

  request(request: T, url: string, id?: string): void {
    if (this.requests.has(request)) return;
    this.requests.set(request, { url, id });
    if (!id || !identityPattern.test(id)) return;
    const requests = this.identities.get(id) ?? new Set<T>();
    requests.add(request);
    this.identities.set(id, requests);
  }

  terminal(request: T): void {
    this.terminalRequests.add(request);
  }

  // START может быть доставлен до goto, а network Request — уже после него.
  // Фиксируем именно существующие identities; прошлый FAIL и новые START не входят.
  navigationStarted(): void {
    for (const [id, event] of this.events) {
      const requests = this.identities.get(id);
      if (
        event.started &&
        !event.invalid &&
        ![...(requests ?? [])].some((request) =>
          this.terminalRequests.has(request),
        )
      )
        this.navigationIdentities.add(id);
    }
  }

  cancelled(request: T): boolean {
    const selected = this.requests.get(request);
    if (!selected?.id || this.identities.get(selected.id)?.size !== 1)
      return false;
    const event = this.events.get(selected.id);
    return (
      !!event?.started &&
      (event.aborted || this.navigationIdentities.has(selected.id)) &&
      !event.invalid &&
      event.url === selected.url
    );
  }

  bodyCompleted(request: T): boolean {
    const selected = this.requests.get(request);
    if (!selected?.id || this.identities.get(selected.id)?.size !== 1)
      return false;
    const event = this.events.get(selected.id);
    return (
      !!event?.started &&
      event.bodyCompleted &&
      !event.aborted &&
      !event.bodyErrored &&
      !event.rejected &&
      !event.invalid &&
      event.url === selected.url
    );
  }

  describe(request: T): string {
    const selected = this.requests.get(request);
    const event = selected?.id ? this.events.get(selected.id) : undefined;
    return `identity=${String(!!selected?.id)}; requests=${String(selected?.id ? (this.identities.get(selected.id)?.size ?? 0) : 0)}; started=${String(!!event?.started)}; aborted=${String(!!event?.aborted)}; navigation=${String(!!selected?.id && this.navigationIdentities.has(selected.id))}; invalid=${String(!!event?.invalid)}`;
  }
}

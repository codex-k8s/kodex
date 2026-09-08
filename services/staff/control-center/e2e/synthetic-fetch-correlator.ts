import type { SyntheticFetchEvent } from "./synthetic-abort-observer";

const identityPattern =
  /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}:[1-9][0-9]{0,6}$/;

// Identity приходит из одного fixture fetch и заголовка именно его network request.
// Совпадение URL/порядка или число одновременно завершившихся запросов не используются.
export class SyntheticFetchCorrelator<T extends object> {
  private readonly events = new Map<
    string,
    { url: string; started: boolean; aborted: boolean; invalid: boolean }
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
      else existing.started = true;
    } else {
      this.events.set(event.id, {
        url: event.url,
        started: event.phase === "start",
        aborted: event.phase === "abort",
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

  cancelled(request: T): boolean {
    const selected = this.requests.get(request);
    if (!selected?.id || this.identities.get(selected.id)?.size !== 1)
      return false;
    const event = this.events.get(selected.id);
    return (
      !!event?.started &&
      event.aborted &&
      !event.invalid &&
      event.url === selected.url
    );
  }

  describe(request: T): string {
    const selected = this.requests.get(request);
    const event = selected?.id ? this.events.get(selected.id) : undefined;
    return `identity=${String(!!selected?.id)}; requests=${String(selected?.id ? (this.identities.get(selected.id)?.size ?? 0) : 0)}; started=${String(!!event?.started)}; aborted=${String(!!event?.aborted)}; invalid=${String(!!event?.invalid)}`;
  }
}

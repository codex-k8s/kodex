import type { Page, Request } from "@playwright/test";

// Подтверждение относится только к tracked logo и замене его документа.
// URL страницы недостаточен: same-URL navigation тоже создаёт новый loader.
export async function observeSyntheticAssetCancellation(
  page: Page,
  browserName: string,
  origin = "https://kodex.test",
) {
  if (browserName !== "chromium")
    return { confirmed: () => false, describe: () => "UNSUPPORTED" };
  const session = await page.context().newCDPSession(page);
  const requests: Request[] = [];
  const pending = new Set<Request>();
  const activeImages = new Set<string>();
  let ambiguous = false;
  page.on("request", (request) => {
    if (
      request.frame() !== page.mainFrame() ||
      request.method() !== "GET" ||
      request.resourceType() !== "image" ||
      request.url() !== `${origin}/logo.png`
    )
      return;
    if (pending.size > 0) ambiguous = true;
    pending.add(request);
    requests.push(request);
  });
  page.on("requestfinished", (request) => pending.delete(request));
  page.on("requestfailed", (request) => pending.delete(request));
  const loaders = new Map<string, string>();
  const transitions: Array<{
    frame: string;
    previous: string;
    next: string;
    start: number;
    commit?: number;
  }> = [];
  const images = new Map<
    string,
    {
      frame: string;
      loader: string;
      url: string;
      failure?: number;
      cancelled?: boolean;
    }
  >();
  let overflow = false;
  session.on("Page.frameNavigated", ({ frame }) => {
    if (!frame.parentId) loaders.set(frame.id, frame.loaderId);
  });
  session.on("Network.requestWillBeSent", (event) => {
    if (overflow || !event.frameId || !event.loaderId) return;
    if (event.type === "Document" && loaders.has(event.frameId)) {
      const previous = loaders.get(event.frameId);
      if (previous && previous !== event.loaderId)
        transitions.push({
          frame: event.frameId,
          previous,
          next: event.loaderId,
          start: event.timestamp,
        });
    }
    if (
      event.type === "Image" &&
      loaders.has(event.frameId) &&
      event.request.method === "GET" &&
      event.request.url === `${origin}/logo.png`
    ) {
      if (activeImages.size > 0 || images.has(event.requestId))
        ambiguous = true;
      activeImages.add(event.requestId);
      images.set(event.requestId, {
        frame: event.frameId,
        loader: event.loaderId,
        url: event.request.url,
      });
    }
    if (transitions.length > 512 || images.size > 512) overflow = true;
  });
  session.on("Page.lifecycleEvent", (event) => {
    if (event.name !== "init") return;
    for (const transition of transitions)
      if (
        transition.frame === event.frameId &&
        transition.next === event.loaderId
      )
        transition.commit = event.timestamp;
  });
  session.on("Network.loadingFailed", (event) => {
    activeImages.delete(event.requestId);
    const image = images.get(event.requestId);
    if (!image) return;
    image.failure = event.timestamp;
    image.cancelled =
      event.canceled === true &&
      event.errorText === "net::ERR_ABORTED" &&
      !event.blockedReason &&
      !event.corsErrorStatus;
  });
  session.on("Network.loadingFinished", (event) =>
    activeImages.delete(event.requestId),
  );
  await session.send("Page.enable");
  await session.send("Page.setLifecycleEventsEnabled", { enabled: true });
  await session.send("Network.enable");
  return {
    describe(request: Request) {
      return JSON.stringify({
        overflow,
        ambiguous,
        ordinal: requests.indexOf(request),
        requestCount: requests.length,
        images: [...images.values()],
        transitions,
      });
    },
    confirmed(request: Request) {
      if (
        overflow ||
        ambiguous ||
        requests.length !== images.size ||
        request.method() !== "GET" ||
        request.resourceType() !== "image" ||
        request.url() !== `${origin}/logo.png` ||
        request.failure()?.errorText !== "net::ERR_ABORTED"
      )
        return false;
      // Последовательные main-frame GET одного exact asset сопоставляются по
      // порядку browser events. Overlap, redirect или потеря события закрыты.
      // Request.timing().startTime до headers может законно оставаться нулём.
      const ordinal = requests.indexOf(request);
      if (ordinal < 0) return false;
      const image = [...images.values()][ordinal];
      if (!image || !image.cancelled || image.failure === undefined)
        return false;
      const failure = image.failure;
      return transitions.some(
        (transition) =>
          transition.frame === image.frame &&
          transition.previous === image.loader &&
          transition.next !== image.loader &&
          transition.commit !== undefined &&
          transition.start <= failure &&
          failure <= transition.commit,
      );
    },
  };
}

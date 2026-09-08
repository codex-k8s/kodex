import { test, type Request } from "@playwright/test";
import {
  installSessionBootstrapObserver,
  SessionBootstrapCorrelator,
  sessionBootstrapIDHeader,
} from "./session-bootstrap-observer";
import {
  SessionRequestDiagnostics,
  type SessionProofStage,
} from "./session-request-diagnostics";
import { createHash } from "node:crypto";
import { persistSessionRenewalEvidence } from "./session-renewal-evidence";
import {
  installProtocolObserver,
  observeFrame,
  renewalWindow,
  resumedAfterRenewal,
  sessionTiming,
  socketProof,
  type SocketProof,
} from "./session-renewal-proof";
import { loadE2ESessionRenewalEnvironment } from "./environment";

const environment = loadE2ESessionRenewalEnvironment();

test("две настоящие вкладки сохраняют ticket/v2 при естественном продлении", async ({
  context,
}, testInfo) => {
  const startedAt = new Date().toISOString();
  const tabs: SocketProof[][] = [[], []];
  const counters = {
    refreshRequests: 0,
    refreshSuccesses: 0,
    ticketRequests: 0,
    ticketSuccesses: 0,
    failedRequests: 0,
    badResponses: 0,
    pageErrors: 0,
    consoleErrors: 0,
    metadataErrors: 0,
  };
  const metadata: ReturnType<typeof sessionTiming>[] = [];
  const pending = new Set<Promise<void>>();
  const bootstrapObservers: SessionBootstrapCorrelator<Request>[] = [];
  const failedRequests: Request[] = [];
  const bootstrapDiagnostics =
    process.env.KODEX_E2E_BOOTSTRAP_SIGNAL_DIAGNOSTICS;
  if (bootstrapDiagnostics !== undefined && bootstrapDiagnostics !== "1")
    throw new Error("Invalid bootstrap signal diagnostic profile");
  let confirmedBootstrapAborts = 0;
  let confirmedRequestSequences: number[] = [];
  let protocolReadback: string[][] = [];
  let stage: SessionProofStage = "PREFLIGHT";
  const requestDiagnostics = new SessionRequestDiagnostics<Request>(
    environment.baseURL,
  );
  let passed = false;
  let initial: ReturnType<typeof renewalWindow> | undefined;
  let naturalRenewalAt = 0;
  const refreshObservedAt: number[] = [];
  const servingManifestSHA256 = process.env.KODEX_E2E_SERVING_MANIFEST_SHA256;
  if (servingManifestSHA256 && !/^[a-f0-9]{64}$/.test(servingManifestSHA256))
    throw new Error("Invalid serving manifest digest");
  const versions = Object.fromEntries(
    ["SOURCE", "API", "PWA"].map((kind) => {
      const value = process.env[`KODEX_E2E_${kind}_REVISION`] ?? "";
      if (!/^[a-f0-9]{40}$/.test(value))
        throw new Error(
          "Session proof requires exact source, API and PWA revisions",
        );
      return [kind.toLowerCase(), value];
    }),
  );
  try {
    const response = await context.request.get("/api/v1/session", {
      failOnStatusCode: false,
      timeout: 15_000,
    });
    if (response.status() !== 200)
      throw new Error("Session metadata unavailable");
    initial = renewalWindow(
      await response.json(),
      environment.runTimeoutMs - 30_000,
    );
    naturalRenewalAt = Date.now() + initial.waitMs;
    await response.dispose();
    // Только наблюдаем выбранный native WebSocket protocol после open. Аргументы,
    // ticket и обработчики приложения не изменяются и не сохраняются.
    await context.addInitScript(installProtocolObserver);
    const pages = await Promise.all([context.newPage(), context.newPage()]);
    for (const [index, page] of pages.entries()) {
      const bootstrapObserver = new SessionBootstrapCorrelator<Request>(
        `${environment.baseURL}/api/v1/bootstrap`,
      );
      bootstrapObservers.push(bootstrapObserver);
      if (bootstrapDiagnostics === "1")
        await installSessionBootstrapObserver(
          page,
          environment.baseURL,
          (event) => bootstrapObserver.observe(event),
        );
      page.on("pageerror", () => counters.pageErrors++);
      page.on("console", (message) => {
        if (message.type() === "error") counters.consoleErrors++;
      });
      page.on("request", (request) => {
        if (bootstrapDiagnostics === "1")
          bootstrapObserver.request(
            request,
            request.url(),
            request.method(),
            request.headers()[sessionBootstrapIDHeader],
          );
        requestDiagnostics.start(request, {
          url: request.url(),
          method: request.method(),
          resourceType: request.resourceType(),
          stage,
          tab: index,
        });
        const url = new URL(request.url());
        if (url.origin !== environment.baseURL) return;
        if (url.pathname === "/api/v1/session" && request.method() === "PUT") {
          counters.refreshRequests++;
          refreshObservedAt.push(Date.now());
        }
        if (
          url.pathname === "/api/v1/session/ticket" &&
          request.method() === "POST"
        )
          counters.ticketRequests++;
      });
      page.on("requestfailed", (request) => {
        counters.failedRequests++;
        failedRequests.push(request);
        bootstrapObserver.failed(request);
        requestDiagnostics.failed(
          request,
          stage,
          request.failure()?.errorText ?? "UNKNOWN",
        );
      });
      page.on("response", (response) => {
        const url = new URL(response.url());
        if (
          url.origin !== environment.baseURL ||
          !url.pathname.startsWith("/api/")
        )
          return;
        if (response.status() >= 400) counters.badResponses++;
        if (
          url.pathname === "/api/v1/session/ticket" &&
          response.request().method() === "POST" &&
          response.status() === 200
        )
          counters.ticketSuccesses++;
        if (url.pathname !== "/api/v1/session" || response.status() !== 200)
          return;
        if (response.request().method() === "PUT") counters.refreshSuccesses++;
        const reading = response
          .json()
          .then((value: unknown) => {
            metadata.push(sessionTiming(value));
          })
          .catch(() => {
            counters.metadataErrors++;
          });
        pending.add(reading);
        void reading.finally(() => pending.delete(reading));
      });
      page.on("websocket", (socket) => {
        if (!new URL(socket.url()).pathname.endsWith("/session/stream")) return;
        const proof = socketProof();
        tabs[index]?.push(proof);
        socket.on("framereceived", ({ payload }) =>
          observeFrame(proof, payload, "received"),
        );
        socket.on("framesent", ({ payload }) =>
          observeFrame(proof, payload, "sent"),
        );
        socket.on("close", () => {
          proof.closed = true;
        });
      });
    }
    stage = "INITIAL_READY";
    await Promise.all(pages.map((page) => page.goto("/")));
    const initialReadyDeadline = Date.now() + 30_000;
    const deadline = naturalRenewalAt + 45_000;
    while (Date.now() < deadline) {
      if (
        Date.now() > initialReadyDeadline &&
        !tabs.every((sockets) => sockets[0]?.ready === 1)
      )
        throw new Error("Initial sockets unavailable");
      if (
        counters.refreshRequests > 1 ||
        counters.badResponses ||
        counters.pageErrors ||
        counters.consoleErrors ||
        counters.metadataErrors
      )
        throw new Error("Session observation failed");
      if (tabs.every(resumedAfterRenewal) && counters.refreshSuccesses === 1)
        break;
      stage = tabs.every((sockets) => sockets[0]?.ready === 1)
        ? "NATURAL_RENEWAL"
        : "INITIAL_READY";
      await new Promise((resolve) => setTimeout(resolve, 500));
    }
    stage = "READBACK";
    // Короткое окно выявляет поздний второй PUT или немедленный reconnect loop.
    await new Promise((resolve) => setTimeout(resolve, 5_000));
    await Promise.all(pending);
    protocolReadback = await Promise.all(
      pages.map((page) =>
        page.evaluate(
          () =>
            (window as unknown as { __kodexSessionProofProtocols: string[] })
              .__kodexSessionProofProtocols,
        ),
      ),
    );
    confirmedBootstrapAborts = failedRequests.filter((request) =>
      bootstrapObservers.some((observer) => observer.cancelled(request)),
    ).length;
    confirmedRequestSequences = failedRequests
      .filter((request) =>
        bootstrapObservers.some((observer) => observer.cancelled(request)),
      )
      .map((request) => requestDiagnostics.sequenceOf(request))
      .filter((sequence): sequence is number => sequence !== undefined)
      .slice(0, 32);
    if (
      !tabs.every(resumedAfterRenewal) ||
      counters.refreshRequests !== 1 ||
      counters.refreshSuccesses !== 1 ||
      refreshObservedAt.some(
        (timestamp) => timestamp < naturalRenewalAt - 1_000,
      ) ||
      counters.ticketRequests < 4 ||
      counters.ticketSuccesses !== counters.ticketRequests ||
      counters.failedRequests !== confirmedBootstrapAborts ||
      requestDiagnostics.snapshot().overflow > 0 ||
      bootstrapObservers.some((observer) => observer.snapshot().overflow > 0) ||
      counters.badResponses ||
      counters.pageErrors ||
      counters.consoleErrors ||
      counters.metadataErrors ||
      protocolReadback.some(
        (values) => values.length < 2 || values.some((value) => value !== "v2"),
      ) ||
      !metadata.some((value) => value.version > Number(initial?.version)) ||
      metadata.some(
        (value) => value.absoluteExpiresAt !== initial?.absoluteExpiresAt,
      )
    )
      throw new Error("Session final readback failed");
    passed = true;
    stage = "COMPLETE";
  } catch {
    // Не передаём Playwright URL auth redirect, response body или исходный error.
    throw new Error(`Session renewal proof failed at ${stage}`);
  } finally {
    const observedTabs = structuredClone(tabs);
    const observedCounters = { ...counters };
    const observedRequests = requestDiagnostics.snapshot();
    // Закрываем страницы до teardown: автоматический error-context не получает DOM.
    await Promise.all(
      context.pages().map((page) => page.close().catch(() => undefined)),
    );
    const evidence = {
      schemaVersion: 1,
      requirement: "MVP-UI-11",
      variant: "two-tabs-natural-renewal-ticket-v2",
      status: passed ? "PASS" : "FAIL",
      stage,
      startedAt,
      finishedAt: new Date().toISOString(),
      browser: testInfo.project.use.browserName,
      versions: {
        harness: versions.source,
        api: versions.api,
        pwa: versions.pwa,
      },
      servingManifestSHA256,
      bootstrapSignalDiagnostics: {
        enabled: bootstrapDiagnostics === "1",
        confirmedBootstrapAborts,
        confirmedRequestSequences,
        unexplainedFailures: counters.failedRequests - confirmedBootstrapAborts,
        tabs: bootstrapObservers.map((observer) => observer.snapshot()),
      },
      naturalRenewalAt: naturalRenewalAt
        ? new Date(naturalRenewalAt).toISOString()
        : undefined,
      refreshObservedAt: refreshObservedAt.map((timestamp) =>
        new Date(timestamp).toISOString(),
      ),
      versionEvidence:
        "operator-supplied; serving readback is a separate prerequisite",
      counters: observedCounters,
      requestDiagnostics: observedRequests,
      protocols: protocolReadback,
      tabs: observedTabs,
      absoluteExpiryUnchanged:
        metadata.length > 0 &&
        metadata.every(
          (value) => value.absoluteExpiresAt === initial?.absoluteExpiresAt,
        ),
      businessEventDelivery:
        "NOT RUN: no controlled producer or expected event set",
      windowSHA256: createHash("sha256")
        .update(
          JSON.stringify({
            startedAt,
            versions,
            tabs: observedTabs,
            counters: observedCounters,
          }),
        )
        .digest("hex"),
    };
    await persistSessionRenewalEvidence(testInfo, evidence);
  }
});

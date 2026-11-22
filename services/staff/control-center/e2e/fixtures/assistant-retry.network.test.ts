import { describe, expect, it } from "vitest";
import type { Page, WebSocketRoute } from "@playwright/test";
import type { RunGraph } from "../../src/shared/api/generated/openapi/types.gen";
import { validateRunSnapshot } from "../../src/features/platform/run-snapshot";
import {
  AssistantRetryNetwork,
  failedRunRef,
  retriedRunRef,
} from "./assistant-retry.network";

describe("owner snapshots браузерной retry fixture", () => {
  for (const scope of ["SYSTEM", "PROJECT"] as const)
    for (const retry of [false, true])
      it(`${scope}: ${retry ? "retry с predecessor" : "исходный запуск"}`, async () => {
        const network = new AssistantRetryNetwork(scope);
        const runRef = retry ? retriedRunRef : failedRunRef;
        if (retry)
          network.runs.set(runRef, network.workspace(runRef, "QUEUED", 2));
        let receive: ((message: string) => void) | undefined;
        const frames: Record<string, unknown>[] = [];
        // Только in-process command port: браузер, сервер и сеть не создаются.
        const socket = {
          onClose() {},
          onMessage(callback: (message: string) => void) {
            receive = callback;
          },
          send(message: string) {
            frames.push(JSON.parse(message) as Record<string, unknown>);
          },
        } as unknown as WebSocketRoute;
        const page = {
          context: () => ({ addCookies: async () => {} }),
          routeWebSocket: (
            _url: string,
            callback: (socket: WebSocketRoute) => void,
          ) => {
            callback(socket);
            return Promise.resolve();
          },
          route: async () => {},
        } as unknown as Page;
        await network.install(page);
        if (!receive)
          throw new Error("Synthetic subscription is not installed");
        receive(
          JSON.stringify({
            type: "SUBSCRIBE_RUN",
            requestRef: "synthetic-run-snapshot",
            runRef,
          }),
        );
        expect(frames).toHaveLength(2);
        const snapshot = frames[0];
        if (!snapshot) throw new Error("Synthetic run snapshot is missing");
        const graph = snapshot.snapshot as RunGraph;
        const checked = validateRunSnapshot(
          graph,
          snapshot.runs,
          "org_retry_fixture",
          {},
          undefined,
        );
        expect(checked.map((run) => run.ref).sort()).toEqual(
          (retry ? [failedRunRef, retriedRunRef] : [failedRunRef]).sort(),
        );
        expect(frames[1]).toMatchObject({
          type: "RUN_READY",
          requestRef: snapshot.requestRef,
          streamRef: runRef,
          cursor: graph.sequence,
        });
      });
});

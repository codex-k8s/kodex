import { afterEach, describe, expect, it, vi } from "vitest";

import type { MutationHeaders } from "@/shared/api/mutation";
import { saveVersionedResource } from "./save-versioned-resource";

afterEach(() => vi.unstubAllGlobals());

describe("saveVersionedResource", () => {
  it("создаёт без If-Match и возвращает точный readback", async () => {
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"a".repeat(43)}`,
    });
    const update = vi.fn();
    const create = vi.fn((headers: MutationHeaders) => {
      expect(headers["If-Match"]).toBeUndefined();
      expect(headers["Idempotency-Key"]).toBeTruthy();
      expect(headers["X-CSRF-Token"]).toBe("a".repeat(43));
      return Promise.resolve({
        data: { ref: "project_created", version: 1 },
        response: new Response(null, { status: 201 }),
      });
    });

    const result = await saveVersionedResource(undefined, update, create);

    expect(result).toEqual({ ref: "project_created", version: 1 });
    expect(create).toHaveBeenCalledOnce();
    expect(update).not.toHaveBeenCalled();
  });

  it("обновляет существующий ресурс с его OCC-версией без создания", async () => {
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"b".repeat(43)}`,
    });
    const current = { ref: "project_existing", version: 7 };
    const update = vi.fn(
      (existing: typeof current, headers: MutationHeaders) => {
        expect(existing).toBe(current);
        expect(headers["If-Match"]).toBe('"7"');
        expect(headers["Idempotency-Key"]).toBeTruthy();
        return Promise.resolve({
          data: { ref: existing.ref, version: 8 },
          response: new Response(null, { status: 200 }),
        });
      },
    );
    const create = vi.fn();

    const result = await saveVersionedResource(current, update, create);

    expect(result).toEqual({ ref: current.ref, version: 8 });
    expect(update).toHaveBeenCalledOnce();
    expect(create).not.toHaveBeenCalled();
  });

  it("не превращает ошибочную версию обновления в создание", async () => {
    vi.stubGlobal("document", {
      cookie: `__Host-kodex-csrf=${"c".repeat(43)}`,
    });
    const update = vi.fn(() =>
      Promise.resolve({
        error: { status: 409, code: "VERSION_CONFLICT" },
        response: new Response(null, { status: 409 }),
      }),
    );
    const create = vi.fn();

    await expect(
      saveVersionedResource({ version: 7 }, update, create),
    ).rejects.toMatchObject({ code: "VERSION_CONFLICT" });

    expect(update).toHaveBeenCalledOnce();
    expect(create).not.toHaveBeenCalled();
  });
});

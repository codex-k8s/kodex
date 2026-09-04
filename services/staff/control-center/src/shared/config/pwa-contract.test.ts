import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("PWA server contract", () => {
  it("публикует installable same-origin manifest", () => {
    const manifest = JSON.parse(
      readFileSync(
        new URL("../../../public/manifest.webmanifest", import.meta.url),
        "utf8",
      ),
    ) as Record<string, unknown>;
    expect(manifest).toMatchObject({
      id: "/",
      start_url: "/",
      scope: "/",
      display: "standalone",
    });
    expect(manifest.icons).toEqual([
      expect.objectContaining({ src: "/logo.png", type: "image/png" }),
    ]);
  });

  it("разрешает microphone только same-origin", () => {
    const headers = readFileSync(
      new URL(
        "../../../../../../deploy/k8s/base/staff-control-center/security-headers.conf",
        import.meta.url,
      ),
      "utf8",
    );
    expect(headers).toContain("microphone=(self)");
    expect(headers).not.toContain("microphone=*");
  });
});

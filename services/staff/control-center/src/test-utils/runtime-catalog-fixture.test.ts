import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
import { overlaySchemaFixture } from "./runtime-catalog-fixture";

it("overlay оснастка совпадает с точным producer golden HTTP", () => {
  const golden: unknown = JSON.parse(
    readFileSync(
      new URL(
        "../../../../external/control-api-gateway/internal/transport/http/testdata/runtime_overlay_schema.json",
        import.meta.url,
      ),
      "utf8",
    ),
  );
  expect(overlaySchemaFixture).toEqual(golden);
});

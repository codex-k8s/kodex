import { describe, expect, it } from "vitest";

import {
  avatarArtifactContentUrl,
  avatarArtifactRef,
  avatarCropTransform,
  avatarMaximumSourceBytes,
  supportedAvatarFile,
} from "@/features/agents/detail/avatar";

describe("avatar lifecycle model", () => {
  it("строит и разбирает только канонический same-origin content URL", () => {
    const url = avatarArtifactContentUrl("art_12345678");
    expect(url).toBe("/api/v1/artifacts/art_12345678/content?purpose=PREVIEW");
    expect(avatarArtifactRef(url)).toBe("art_12345678");
    expect(
      avatarArtifactRef("https://example.invalid/avatar.png"),
    ).toBeUndefined();
    expect(
      avatarArtifactRef(
        "/api/v1/artifacts/art_12345678/content?purpose=DOWNLOAD",
      ),
    ).toBeUndefined();
  });

  it("сохраняет квадрат полностью покрытым изображением при zoom и смещении", () => {
    expect(avatarCropTransform(1200, 600, 512, 1, { x: 900, y: 900 })).toEqual({
      x: 256,
      y: 0,
      drawWidth: 1024,
      drawHeight: 512,
      scale: 512 / 600,
    });
    const zoomed = avatarCropTransform(600, 1200, 512, 2, {
      x: -900,
      y: -900,
    });
    expect(zoomed.x).toBe(-256);
    expect(zoomed.y).toBe(-768);
    expect(zoomed.drawWidth).toBe(1024);
    expect(zoomed.drawHeight).toBe(2048);
  });

  it("отклоняет неподдерживаемый или слишком большой исходник", () => {
    expect(
      supportedAvatarFile(
        new File(["png"], "avatar.png", { type: "image/png" }),
      ),
    ).toBe(true);
    expect(
      supportedAvatarFile(
        new File(["svg"], "avatar.svg", { type: "image/svg+xml" }),
      ),
    ).toBe(false);
    expect(
      supportedAvatarFile(
        new File([new Uint8Array(avatarMaximumSourceBytes + 1)], "large.png", {
          type: "image/png",
        }),
      ),
    ).toBe(false);
  });
});

const avatarContentPrefix = "/api/v1/artifacts/";

export const avatarOutputSize = 512;
export const avatarMaximumSourceBytes = 10 * 1024 * 1024;
export const avatarMaximumDimension = 8192;

export interface AvatarCropOffset {
  x: number;
  y: number;
}

export interface AvatarCropTransform extends AvatarCropOffset {
  drawWidth: number;
  drawHeight: number;
  scale: number;
}

export function avatarArtifactContentUrl(artifactRef: string): string {
  if (!/^[A-Za-z0-9_-]{8,96}$/.test(artifactRef))
    throw new Error("Invalid avatar artifact reference");
  return `${avatarContentPrefix}${artifactRef}/content?purpose=PREVIEW`;
}

export function avatarArtifactRef(value?: string): string | undefined {
  const source = value?.trim();
  if (!source) return undefined;
  const match =
    /^\/api\/v1\/artifacts\/([A-Za-z0-9_-]{8,96})\/content\?purpose=PREVIEW$/.exec(
      source,
    );
  return match?.[1];
}

export function supportedAvatarFile(file: File): boolean {
  return (
    ["image/jpeg", "image/png", "image/webp"].includes(file.type) &&
    file.size > 0 &&
    file.size <= avatarMaximumSourceBytes
  );
}

export function clampAvatarCropOffset(
  imageWidth: number,
  imageHeight: number,
  frameSize: number,
  zoom: number,
  offset: AvatarCropOffset,
): AvatarCropOffset {
  const scale =
    Math.max(frameSize / imageWidth, frameSize / imageHeight) * zoom;
  const maximumX = Math.max(0, (imageWidth * scale - frameSize) / 2);
  const maximumY = Math.max(0, (imageHeight * scale - frameSize) / 2);
  return {
    x: Math.max(-maximumX, Math.min(maximumX, offset.x)),
    y: Math.max(-maximumY, Math.min(maximumY, offset.y)),
  };
}

export function avatarCropTransform(
  imageWidth: number,
  imageHeight: number,
  frameSize: number,
  zoom: number,
  offset: AvatarCropOffset,
): AvatarCropTransform {
  const boundedZoom = Math.max(1, Math.min(3, zoom));
  const scale =
    Math.max(frameSize / imageWidth, frameSize / imageHeight) * boundedZoom;
  const bounded = clampAvatarCropOffset(
    imageWidth,
    imageHeight,
    frameSize,
    boundedZoom,
    offset,
  );
  return {
    ...bounded,
    drawWidth: imageWidth * scale,
    drawHeight: imageHeight * scale,
    scale,
  };
}

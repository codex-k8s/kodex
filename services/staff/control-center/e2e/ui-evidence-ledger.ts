import {
  safeVariant,
  type Variant,
  type Versions,
} from "./ui-acceptance-proof";
export interface UIObservation {
  browser: "chromium" | "firefox" | "webkit";
  fixtureManifestSHA256: string;
  versions: Versions;
  variant: Variant;
  artifactSHA256: string;
}
// Вариант другого движка или другого набора pins не заменяет прежний результат.
export function observationKey(
  value: UIObservation,
  requirement: string,
): string {
  if (
    !value.variant.requirements.includes(requirement) ||
    !["chromium", "firefox", "webkit"].includes(value.browser) ||
    !/^(?:[a-f0-9]{64})?$/.test(value.fixtureManifestSHA256) ||
    !/^[a-f0-9]{64}$/.test(value.artifactSHA256)
  )
    throw new Error("Invalid evidence identity");
  safeVariant(value.variant);
  return [
    value.browser,
    requirement,
    value.variant.id,
    value.fixtureManifestSHA256 || "discovery",
  ].join(":");
}
export function latestObservations(
  values: readonly UIObservation[],
): Map<string, UIObservation> {
  const latest = new Map<string, UIObservation>();
  for (const value of [...values].sort((a, b) =>
    a.variant.timestampUTC.localeCompare(b.variant.timestampUTC),
  ))
    for (const requirement of value.variant.requirements)
      latest.set(observationKey(value, requirement), value);
  return latest;
}

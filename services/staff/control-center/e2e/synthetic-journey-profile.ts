export const mobileJourneyParts = [
  "CORE_ENV",
  "PROVIDERS_INTEGRATIONS",
  "WORKFLOW_CONTEXT",
  "CATALOGS",
  "FILES_HISTORY",
] as const;
export type MobileJourneyPart = (typeof mobileJourneyParts)[number];
export type JourneyPart = "ALL" | MobileJourneyPart;
export interface SyntheticJourneyProfile {
  width: number;
  height: number;
  part: JourneyPart;
  title: string;
}

// Каждый descriptor создаёт отдельный Playwright test с новым context и теми же factories.
export function syntheticJourneyProfiles(): SyntheticJourneyProfile[] {
  return [
    ...[
      { width: 2900, height: 1600 },
      { width: 2560, height: 1440 },
      { width: 1920, height: 1080 },
      { width: 1440, height: 900 },
      { width: 1280, height: 900 },
      { width: 900, height: 1080 },
    ].map(({ width, height }) => ({
      width,
      height,
      part: "ALL" as const,
      title: `synthetic: Home и ассистент ${String(width)}px`,
    })),
    ...mobileJourneyParts.map((part) => ({
      width: 390,
      height: 844,
      part,
      title: `synthetic: mobile390 ${part}`,
    })),
    ...mobileJourneyParts.map((part) => ({
      width: 768,
      height: 1024,
      part,
      title: `synthetic: tablet768 ${part}`,
    })),
  ];
}
export function includesJourneySection(
  part: JourneyPart,
  section: MobileJourneyPart,
): boolean {
  return part === "ALL" || part === section;
}
export function expectedJourneyDiagnostics(
  part: JourneyPart,
  width: number,
): { snapshotConflicts: number; publicationTimeouts: number } {
  return {
    snapshotConflicts:
      includesJourneySection(part, "FILES_HISTORY") &&
      (width === 390 || width === 768 || width === 2900)
        ? 1
        : 0,
    publicationTimeouts: includesJourneySection(part, "CORE_ENV") ? 2 : 0,
  };
}

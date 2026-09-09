export function expectedSyntheticHTTPFailure(
  address: string,
  status: number,
  inspectorStatus?: number,
): "snapshot" | "publication" | "inspector" | undefined {
  let url: URL;
  try {
    url = new URL(address);
  } catch {
    return;
  }
  if (url.origin !== "https://kodex.test") return;
  if (
    status === 412 &&
    url.pathname === "/api/v1/runs" &&
    url.searchParams.get("resumableSessionsOnly") === "true" &&
    url.searchParams.get("pageToken") === "session-snapshot"
  )
    return "snapshot";
  if (
    status === 504 &&
    /^\/api\/v1\/(prompt-template-configurations\/configuration_synthetic\/revisions\/[^/]+|runtime-environment-drafts\/draft_prepared_environment)\/publication$/.test(
      url.pathname,
    )
  )
    return "publication";
  if (
    status === inspectorStatus &&
    [404, 503].includes(status) &&
    /^\/api\/v1\/runtime-environments\/environment_synthetic\/(readiness|agents)$/.test(
      url.pathname,
    )
  )
    return "inspector";
}

export function browserHTTPConsoleStatus(text: string): number | undefined {
  const match =
    /^Failed to load resource: the server responded with a status of (\d{3}) \([^\n]*\)$/.exec(
      text,
    );
  return match ? Number(match[1]) : undefined;
}

export function isFirefoxScrollAdvisory(
  browserName: string,
  text: string,
): boolean {
  return (
    browserName === "firefox" &&
    /^\[JavaScript Warning: "This site appears to use a scroll-linked positioning effect\. This may not work well with asynchronous panning; see https:\/\/firefox-source-docs\.mozilla\.org\/performance\/scroll-linked_effects\.html for further details and to join the discussion on related tools and features!" \{file: "https:\/\/kodex\.test\/(?:e2e\/fixtures\/[a-z-]+\.html|projects\/project_synthetic_0\/workflows\/workflow_synthetic)" line: 0\}\]$/.test(
      text,
    )
  );
}

export function isFirefoxBounceTrackerAdvisory(
  browserName: string,
  type: string,
  source: string,
  line: number,
  column: number,
  text: string,
): boolean {
  return (
    browserName === "firefox" &&
    type === "warning" &&
    source === "" &&
    line === 0 &&
    column === 0 &&
    text ===
      '[JavaScript Warning: "“identity.invalid” has been classified as a bounce tracker. If it does not receive user activation within the next 3,600 seconds it will have its state purged."]'
  );
}

export function isWebKitFontAdvisory(
  browserName: string,
  text: string,
): boolean {
  return (
    browserName === "webkit" &&
    /^The resource https:\/\/kodex\.test\/fonts\/ibm-plex-sans-(cyrillic|latin)-400-normal\.woff2 was preloaded using link preload but not used within a few seconds from the window's load event\. Please make sure it wasn't preloaded for nothing\.$/.test(
      text,
    )
  );
}

export function isConfirmedSyntheticCancellation(
  browserName: string,
  code: string,
  cancelledByScenario: boolean,
): boolean {
  if (!cancelledByScenario) return false;
  const expected: Record<string, string> = {
    chromium: "net::ERR_ABORTED",
    firefox: "NS_BINDING_ABORTED",
    webkit: "Load request cancelled",
  };
  return expected[browserName] === code;
}

export function isCompletedChromiumTicketTerminal(
  browserName: string,
  code: string,
  method: string,
  resourceType: string,
  pathname: string,
  bodyCompleted: boolean,
): boolean {
  return (
    browserName === "chromium" &&
    code === "net::ERR_ABORTED" &&
    method === "POST" &&
    resourceType === "fetch" &&
    pathname === "/api/v1/session/ticket" &&
    bodyCompleted
  );
}

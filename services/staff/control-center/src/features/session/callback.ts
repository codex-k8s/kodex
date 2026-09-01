import type { LoginCompletion } from "./store";

export function callbackReturnPath(
  completion: LoginCompletion,
  onboardingComplete?: boolean,
): string {
  if (completion.kind !== "login") {
    if (!completion.returnPath)
      throw new Error("OIDC re-auth return path is unavailable");
    return completion.returnPath;
  }
  return onboardingComplete ? "/" : "/onboarding";
}

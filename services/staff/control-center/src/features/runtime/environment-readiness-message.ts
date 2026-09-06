export function environmentReadinessMessage(
  code: string,
  t: (key: string) => string,
): string {
  switch (code) {
    case "ENVIRONMENT_NOT_ACTIVE":
      return t("runtime.environmentNotActive");
    case "PUBLISHED_VERSION_MISSING":
      return t("runtime.environmentPublishedMissing");
    case "PROMOTED_IMAGE_MISSING":
      return t("runtime.environmentPromotedMissing");
    case "ROLE_RUNTIME_CONTRACT_STALE":
      return t("runtime.environmentContractStale");
    default:
      return t("runtime.environmentReadinessUnknown");
  }
}

export function needsEnvironmentBinding(
  currentEnvironmentRef: string,
  currentVersionRef: string | undefined,
  selectedEnvironmentRef: string,
  selectedVersionRef: string,
  selectedReady: boolean,
): boolean {
  return (
    selectedReady &&
    Boolean(selectedEnvironmentRef && selectedVersionRef) &&
    (selectedEnvironmentRef !== currentEnvironmentRef ||
      selectedVersionRef !== currentVersionRef)
  );
}

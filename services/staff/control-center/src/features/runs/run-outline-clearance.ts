export function observeRunOutlineClearance(
  summary: HTMLElement,
  onChange: (bottom: number) => void,
): () => void {
  const workspace = summary.parentElement;
  if (!workspace) return () => {};
  const measure = (): void => {
    const bottom =
      summary.getBoundingClientRect().bottom -
      workspace.getBoundingClientRect().top;
    if (Number.isFinite(bottom)) onChange(Math.ceil(Math.max(0, bottom)));
  };
  measure();
  if (typeof ResizeObserver === "undefined") return () => {};
  const observer = new ResizeObserver(measure);
  observer.observe(summary);
  observer.observe(workspace);
  return () => observer.disconnect();
}

const focusableSelector = [
  'a[href]:not([tabindex="-1"])',
  'button:not([disabled]):not([tabindex="-1"])',
  'input:not([disabled]):not([type="hidden"]):not([tabindex="-1"])',
  'select:not([disabled]):not([tabindex="-1"])',
  'textarea:not([disabled]):not([tabindex="-1"])',
  '[tabindex]:not([tabindex="-1"])',
].join(",");

export function focusableElements(container: HTMLElement): HTMLElement[] {
  return Array.from(container.querySelectorAll<HTMLElement>(focusableSelector));
}

export function trappedFocusTarget(
  elements: readonly HTMLElement[],
  activeElement: Element | null,
  backwards: boolean,
): HTMLElement | undefined {
  if (elements.length === 0) return undefined;
  const current = elements.indexOf(activeElement as HTMLElement);
  if (current < 0) return backwards ? elements.at(-1) : elements[0];
  if (backwards && current === 0) return elements.at(-1);
  if (!backwards && current === elements.length - 1) return elements[0];
  return undefined;
}

/// <reference types="vite/client" />

import type { AutomationCallbackReceipt } from "./features/automation-history/contract";

declare global {
  interface Window {
    __MATTERCODEX_AUTOMATION_HISTORY__?: readonly AutomationCallbackReceipt[];
  }
}

export {};

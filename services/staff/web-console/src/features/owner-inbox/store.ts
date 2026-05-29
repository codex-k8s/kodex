import { defineStore } from 'pinia';

import {
  fetchOwnerInboxItem,
  fetchOwnerInboxItems,
  sendOwnerInboxResponse,
} from '@/shared/api/staff-gateway';
import type {
  OwnerInboxItem,
  OwnerInboxResponseSummary,
  RequestKind,
  RequestStatus,
  ResponseAction,
} from '@/shared/api/generated';
import type { ApiError } from '@/shared/api/errors';
import type { OperatorContext } from '@/shared/api/context';
import { createRequestId } from '@/shared/lib/request';

export type OwnerInboxFilters = {
  kinds: RequestKind[];
  statuses: RequestStatus[];
  includeDiagnostics: boolean;
  pageSize: number;
};

export const terminalRequestStatuses: RequestStatus[] = ['answered', 'expired', 'cancelled', 'failed'];

export const useOwnerInboxStore = defineStore('owner-inbox', {
  state: () => ({
    items: [] as OwnerInboxItem[],
    selectedItem: undefined as OwnerInboxItem | undefined,
    latestResponse: undefined as OwnerInboxResponseSummary | undefined,
    pageToken: undefined as string | undefined,
    nextPageToken: undefined as string | undefined,
    filters: {
      kinds: [] as RequestKind[],
      statuses: ['waiting', 'routed', 'created'] as RequestStatus[],
      includeDiagnostics: true,
      pageSize: 25,
    } satisfies OwnerInboxFilters,
    isLoadingList: false,
    isLoadingDetail: false,
    isResponding: false,
    error: undefined as ApiError | undefined,
  }),
  getters: {
    pendingCount: (state) =>
      state.items.filter((item) => !terminalRequestStatuses.includes(item.request_status)).length,
    selectedAllowedActions: (state) => {
      if (!state.selectedItem || terminalRequestStatuses.includes(state.selectedItem.request_status)) {
        return [];
      }
      return state.selectedItem.allowed_actions;
    },
  },
  actions: {
    async load(context: OperatorContext, pageToken?: string) {
      this.isLoadingList = true;
      this.error = undefined;
      try {
        const response = await fetchOwnerInboxItems(context, {
          requestKinds: this.filters.kinds.length > 0 ? this.filters.kinds : undefined,
          statuses: this.filters.statuses.length > 0 ? this.filters.statuses : undefined,
          includeDiagnostics: this.filters.includeDiagnostics,
          pageSize: this.filters.pageSize,
          pageToken,
        });
        this.items = response.items;
        this.nextPageToken = response.page.next_page_token;
        this.pageToken = pageToken;
        if (this.items.length > 0 && !this.selectedItem) {
          await this.select(context, this.items[0].request_id);
        }
        if (this.items.length === 0) {
          this.selectedItem = undefined;
        }
      } catch (error) {
        this.error = error as ApiError;
      } finally {
        this.isLoadingList = false;
      }
    },
    async select(context: OperatorContext, requestId: string) {
      this.isLoadingDetail = true;
      this.error = undefined;
      try {
        const response = await fetchOwnerInboxItem(
          context,
          requestId,
          this.filters.includeDiagnostics,
        );
        this.selectedItem = response.item;
      } catch (error) {
        this.error = error as ApiError;
      } finally {
        this.isLoadingDetail = false;
      }
    },
    async respond(
      context: OperatorContext,
      action: ResponseAction,
      responseSummary: string,
      reason: string,
    ) {
      if (!this.selectedItem) {
        return;
      }
      this.isResponding = true;
      this.error = undefined;
      try {
        const response = await sendOwnerInboxResponse(context, this.selectedItem.request_id, {
          action,
          expected_version: this.selectedItem.version,
          command_id: createRequestId('owner-action'),
          response_summary: responseSummary.trim() || undefined,
          reason: reason.trim() || undefined,
        });
        this.selectedItem = response.item;
        this.latestResponse = response.response;
        const index = this.items.findIndex((item) => item.request_id === response.item.request_id);
        if (index >= 0) {
          this.items.splice(index, 1, response.item);
        }
      } catch (error) {
        this.error = error as ApiError;
      } finally {
        this.isResponding = false;
      }
    },
  },
});

import type {
  MissionControlActionPresentation,
  MissionControlDeepLinkPresentation,
  MissionControlEntityCard,
  MissionControlEntityDetails,
  MissionControlEntityKind,
  MissionControlInfoRow,
} from "./types";

type WorkItemLikePayload = {
  issue_number: number;
  repository_full_name?: string;
  issue_url?: string | null;
  last_run_id?: string | null;
  last_status?: string | null;
  trigger_kind?: string | null;
  work_item_type?: string | null;
  stage_label?: string | null;
  labels?: string[];
  owner?: string | null;
  assignees?: string[];
  last_provider_sync_at?: string | null;
};

type DiscussionLikePayload = {
  discussion_kind: string;
  status?: string | null;
  author?: string | null;
  participant_count?: number;
  latest_comment_excerpt?: string | null;
  formalization_target?: string | null;
};

type PullRequestLikePayload = {
  pull_request_number: number;
  repository_full_name?: string;
  pull_request_url?: string | null;
  last_run_id?: string | null;
  last_status?: string | null;
  branch_head?: string | null;
  branch_base?: string | null;
  merge_state?: string | null;
  review_decision?: string | null;
  checks_summary?: string | null;
  linked_issue_refs?: string[];
};

type AgentLikePayload = {
  agent_key: string;
  run_status?: string | null;
  runtime_mode?: string | null;
  waiting_reason?: string | null;
  active_run_id?: string | null;
  last_heartbeat_at?: string | null;
  last_run_repository?: string | null;
};

function isWorkItemPayload(payload: MissionControlEntityDetails["detail_payload"]): payload is WorkItemLikePayload {
  return "issue_number" in payload;
}

function isDiscussionPayload(payload: MissionControlEntityDetails["detail_payload"]): payload is DiscussionLikePayload {
  return "discussion_kind" in payload;
}

function isPullRequestPayload(payload: MissionControlEntityDetails["detail_payload"]): payload is PullRequestLikePayload {
  return "pull_request_number" in payload;
}

function isAgentPayload(payload: MissionControlEntityDetails["detail_payload"]): payload is AgentLikePayload {
  return "agent_key" in payload;
}

export function missionControlEntityKindLabelKey(kind: MissionControlEntityKind): string {
  switch (kind) {
    case "work_item":
      return "pages.missionControl.entityKinds.work_item";
    case "discussion":
      return "pages.missionControl.entityKinds.discussion";
    case "pull_request":
      return "pages.missionControl.entityKinds.pull_request";
    case "agent":
      return "pages.missionControl.entityKinds.agent";
  }
}

export function missionControlStateLabelKey(state: MissionControlEntityCard["state"]): string {
  return `pages.missionControl.states.${state}`;
}

export function missionControlStateColor(state: MissionControlEntityCard["state"]): string {
  switch (state) {
    case "working":
      return "info";
    case "waiting":
      return "warning";
    case "blocked":
      return "error";
    case "review":
      return "success";
    case "recent_critical_updates":
      return "secondary";
  }
}

export function missionControlSyncStatusLabelKey(status: MissionControlEntityCard["sync_status"]): string {
  return `pages.missionControl.sync.${status}`;
}

export function missionControlSyncStatusColor(status: MissionControlEntityCard["sync_status"]): string {
  switch (status) {
    case "synced":
      return "success";
    case "pending_sync":
      return "warning";
    case "failed":
      return "error";
    case "degraded":
      return "secondary";
  }
}

export function missionControlBadgeLabelKey(badge: MissionControlEntityCard["badges"][number]): string {
  return `pages.missionControl.badges.${badge}`;
}

export function missionControlActionLabelKey(actionKind: string): string {
  return `pages.missionControl.actions.${actionKind}`;
}

export function missionControlActionPresentation(actionKind: string): MissionControlActionPresentation {
  switch (actionKind) {
    case "stage.next_step.execute":
      return { color: "primary", icon: "mdi-rocket-launch-outline" };
    case "discussion.create":
      return { color: "info", icon: "mdi-comment-plus-outline" };
    case "work_item.create":
      return { color: "success", icon: "mdi-briefcase-plus-outline" };
    case "discussion.formalize":
      return { color: "secondary", icon: "mdi-file-document-edit-outline" };
    case "command.retry_sync":
      return { color: "warning", icon: "mdi-refresh" };
    default:
      return { color: "secondary", icon: "mdi-dots-horizontal" };
  }
}

export function missionControlDeepLinkPresentation(actionKind: string): MissionControlDeepLinkPresentation {
  switch (actionKind) {
    case "provider.open_pr":
      return { color: "success", icon: "mdi-source-pull", labelKey: "pages.missionControl.deepLinks.provider.open_pr" };
    case "provider.review":
      return { color: "warning", icon: "mdi-comment-check-outline", labelKey: "pages.missionControl.deepLinks.provider.review" };
    case "provider.merge":
      return { color: "warning", icon: "mdi-source-merge", labelKey: "pages.missionControl.deepLinks.provider.merge" };
    case "provider.reply_comment":
      return { color: "info", icon: "mdi-comment-edit-outline", labelKey: "pages.missionControl.deepLinks.provider.reply_comment" };
    default:
      return { color: "primary", icon: "mdi-open-in-new", labelKey: "pages.missionControl.deepLinks.provider.open_issue" };
  }
}

function pushInfoRow(
  rows: MissionControlInfoRow[],
  labelKey: string,
  value: string | null | undefined,
  options: { mono?: boolean; href?: string } = {},
): void {
  const trimmed = String(value || "").trim();
  if (trimmed === "") return;
  rows.push({
    labelKey,
    value: trimmed,
    mono: options.mono,
    href: options.href,
  });
}

export function buildMissionControlInfoRows(details: MissionControlEntityDetails): MissionControlInfoRow[] {
  const rows: MissionControlInfoRow[] = [];
  const payload = details.detail_payload;

  if (isWorkItemPayload(payload)) {
    pushInfoRow(rows, "pages.missionControl.sidePanel.repository", payload.repository_full_name, { mono: true });
    pushInfoRow(rows, "pages.missionControl.sidePanel.issueNumber", `#${payload.issue_number}`, {
      mono: true,
      href: payload.issue_url || undefined,
    });
    pushInfoRow(rows, "pages.missionControl.sidePanel.stageLabel", payload.stage_label, { mono: true });
    pushInfoRow(rows, "pages.missionControl.sidePanel.workItemType", payload.work_item_type);
    pushInfoRow(rows, "pages.missionControl.sidePanel.owner", payload.owner);
    pushInfoRow(rows, "pages.missionControl.sidePanel.triggerKind", payload.trigger_kind, { mono: true });
    pushInfoRow(rows, "pages.missionControl.sidePanel.lastRun", payload.last_run_id, { mono: true });
    pushInfoRow(rows, "pages.missionControl.sidePanel.lastStatus", payload.last_status);
  }

  if (isDiscussionPayload(payload)) {
    pushInfoRow(rows, "pages.missionControl.sidePanel.discussionKind", payload.discussion_kind);
    pushInfoRow(rows, "pages.missionControl.sidePanel.status", payload.status);
    pushInfoRow(rows, "pages.missionControl.sidePanel.author", payload.author);
    if (typeof payload.participant_count === "number") {
      pushInfoRow(rows, "pages.missionControl.sidePanel.participants", String(payload.participant_count));
    }
    pushInfoRow(rows, "pages.missionControl.sidePanel.formalizationTarget", payload.formalization_target);
  }

  if (isPullRequestPayload(payload)) {
    pushInfoRow(rows, "pages.missionControl.sidePanel.repository", payload.repository_full_name, { mono: true });
    pushInfoRow(rows, "pages.missionControl.sidePanel.pullRequestNumber", `#${payload.pull_request_number}`, {
      mono: true,
      href: payload.pull_request_url || undefined,
    });
    pushInfoRow(rows, "pages.missionControl.sidePanel.branchHead", payload.branch_head, { mono: true });
    pushInfoRow(rows, "pages.missionControl.sidePanel.branchBase", payload.branch_base, { mono: true });
    pushInfoRow(rows, "pages.missionControl.sidePanel.mergeState", payload.merge_state);
    pushInfoRow(rows, "pages.missionControl.sidePanel.reviewDecision", payload.review_decision);
    pushInfoRow(rows, "pages.missionControl.sidePanel.checksSummary", payload.checks_summary);
    pushInfoRow(rows, "pages.missionControl.sidePanel.lastRun", payload.last_run_id, { mono: true });
    pushInfoRow(rows, "pages.missionControl.sidePanel.lastStatus", payload.last_status);
  }

  if (isAgentPayload(payload)) {
    pushInfoRow(rows, "pages.missionControl.sidePanel.agentKey", payload.agent_key, { mono: true });
    pushInfoRow(rows, "pages.missionControl.sidePanel.runtimeMode", payload.runtime_mode, { mono: true });
    pushInfoRow(rows, "pages.missionControl.sidePanel.runStatus", payload.run_status);
    pushInfoRow(rows, "pages.missionControl.sidePanel.waitingReason", payload.waiting_reason);
    pushInfoRow(rows, "pages.missionControl.sidePanel.lastRun", payload.active_run_id, { mono: true });
    pushInfoRow(rows, "pages.missionControl.sidePanel.lastRepository", payload.last_run_repository, { mono: true });
  }

  return rows;
}

export function missionControlWorkItemLabels(details: MissionControlEntityDetails): string[] {
  return isWorkItemPayload(details.detail_payload) ? details.detail_payload.labels ?? [] : [];
}

export function missionControlWorkItemAssignees(details: MissionControlEntityDetails): string[] {
  return isWorkItemPayload(details.detail_payload) ? details.detail_payload.assignees ?? [] : [];
}

export function missionControlPullRequestLinkedIssues(details: MissionControlEntityDetails): string[] {
  return isPullRequestPayload(details.detail_payload) ? details.detail_payload.linked_issue_refs ?? [] : [];
}

export function missionControlDiscussionLatestComment(details: MissionControlEntityDetails): string {
  return isDiscussionPayload(details.detail_payload) ? String(details.detail_payload.latest_comment_excerpt || "") : "";
}

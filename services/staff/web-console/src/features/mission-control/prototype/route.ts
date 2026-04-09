import type { LocationQuery, LocationQueryRaw } from "vue-router";

import type { MissionControlPrototypeRouteState, MissionDrawerTab } from "./types";

const missionPrototypeDefaultDrawerTab: MissionDrawerTab = "details";

function asQueryString(value: LocationQuery[string]): string {
  if (typeof value === "string") {
    return value.trim();
  }
  if (Array.isArray(value) && typeof value[0] === "string") {
    return value[0].trim();
  }
  return "";
}

function isDrawerTab(value: string): value is MissionDrawerTab {
  return value === "details" || value === "timeline" || value === "workflow";
}

export function normalizeMissionControlPrototypeRouteQuery(query: LocationQuery): MissionControlPrototypeRouteState {
  const rawTab = asQueryString(query.tab);

  return {
    scenarioId: asQueryString(query.scenario),
    initiativeId: asQueryString(query.initiative),
    nodeId: asQueryString(query.node),
    search: asQueryString(query.q),
    tab: isDrawerTab(rawTab) ? rawTab : missionPrototypeDefaultDrawerTab,
  };
}

export function buildMissionControlPrototypeRouteQuery(
  state: MissionControlPrototypeRouteState,
  defaults: { scenarioId: string },
): LocationQueryRaw {
  return {
    scenario: state.scenarioId !== "" && state.scenarioId !== defaults.scenarioId ? state.scenarioId : undefined,
    initiative: state.initiativeId || undefined,
    node: state.nodeId || undefined,
    q: state.search || undefined,
    tab: state.tab !== missionPrototypeDefaultDrawerTab ? state.tab : undefined,
  };
}

export function patchMissionControlPrototypeRouteState(
  current: MissionControlPrototypeRouteState,
  patch: Partial<MissionControlPrototypeRouteState>,
): MissionControlPrototypeRouteState {
  return {
    ...current,
    ...patch,
  };
}

export function missionControlPrototypeRouteStateEquals(
  left: MissionControlPrototypeRouteState,
  right: MissionControlPrototypeRouteState,
): boolean {
  return (
    left.scenarioId === right.scenarioId &&
    left.initiativeId === right.initiativeId &&
    left.nodeId === right.nodeId &&
    left.search === right.search &&
    left.tab === right.tab
  );
}

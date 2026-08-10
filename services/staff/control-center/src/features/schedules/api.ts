import {
  createOwnerSchedule,
  deleteOwnerSchedule,
  fetchOwnerSchedules,
  fetchScheduleOccurrences,
  fetchScheduleSelectors,
  recoverScheduleOccurrence,
  runOwnerScheduleNow,
  updateOwnerSchedule,
} from "@/shared/api/adapters/owner-control";
import {
  fetchResource,
  fetchResources,
  transitionMutableResource,
} from "@/shared/api/adapters/resources";

export const schedulesApi = {
  list: fetchOwnerSchedules,
  selectors: fetchScheduleSelectors,
  occurrences: fetchScheduleOccurrences,
  create: createOwnerSchedule,
  update: updateOwnerSchedule,
  runNow: runOwnerScheduleNow,
  remove: deleteOwnerSchedule,
  recover: recoverScheduleOccurrence,
  getResource: fetchResource,
  listResources: fetchResources,
  transition: transitionMutableResource,
};

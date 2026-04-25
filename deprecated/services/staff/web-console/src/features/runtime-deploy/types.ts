import type { RealtimePagination } from "../runs/types";
import type {
  RuntimeDeployTaskActionResponse,
  RuntimeDeployTaskListItem,
  RuntimeDeployTask,
  RuntimeDeployTaskLog,
} from "../../shared/api/generated";

export type {
  RuntimeDeployTaskActionResponse,
  RuntimeDeployTaskListItem,
  RuntimeDeployTask,
  RuntimeDeployTaskLog,
};

export type RuntimeDeployTasksRealtimeMessageType = "snapshot" | "error";

export type RuntimeDeployTasksRealtimeMessage = {
  type: RuntimeDeployTasksRealtimeMessageType;
  items?: RuntimeDeployTaskListItem[];
  pagination?: RealtimePagination;
  message?: string;
  sent_at: string;
};

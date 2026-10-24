// Thin client for the planner API. Every call goes to the same origin; the
// session is an HttpOnly cookie the browser sends automatically.

export const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL ?? "/api/v1";

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

export async function api<T>(path: string, init?: { method?: string; body?: unknown }): Promise<T> {
  const res = await fetch(API_BASE + path, {
    method: init?.method ?? "GET",
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      // Required by the API on every write as a CSRF guard.
      "X-Requested-With": "planner",
    },
    body: init?.body === undefined ? undefined : JSON.stringify(init.body),
    cache: "no-store",
  });
  if (res.status === 401 && typeof window !== "undefined" && !window.location.pathname.startsWith("/login")) {
    window.location.href = "/login?next=" + encodeURIComponent(window.location.pathname + window.location.search);
  }
  if (!res.ok) {
    let msg = res.statusText;
    try {
      msg = (await res.json()).error ?? msg;
    } catch {}
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export type Role = "admin" | "cto" | "project_manager" | "tech_lead" | "viewer";
export const ROLE_LABEL: Record<Role, string> = {
  admin: "Administrator",
  cto: "CTO",
  project_manager: "Project manager",
  tech_lead: "Tech lead",
  viewer: "Viewer",
};

export interface User {
  id: number;
  email: string;
  display_name: string;
  role: Role;
  disabled: boolean;
  created_at: string;
  last_login_at: string | null;
}

export interface Me {
  user: User;
  permissions: string[];
  all_projects: boolean;
  project_ids: number[];
}

export type HealthState = "on_track" | "at_risk" | "blocked" | "insufficient_data";
export type Severity = "critical" | "high" | "medium" | "low";

export interface MilestoneRef {
  id: number;
  project_id: number;
  name: string;
  status: string;
  due_date: string | null;
  open: number;
  total: number;
  // Created in the planner, not in Redmine. Planner milestones have negative IDs.
  planner: boolean;
}

export interface DeliveryDates {
  baseline: string | null;
  baseline_source: string | null;
  forecast: string | null;
  actual: string | null;
  confidence: "high" | "medium" | "low" | "none";
  method: string;
  assumptions: string[];
  inputs: Record<string, string | number | null>;
  status: "on_track" | "at_risk" | "late" | "tbd" | "delivered";
}

export interface ProjectSummary {
  id: number;
  name: string;
  identifier: string;
  parent_id: number | null;
  last_synced_at: string;
  owner: string | null;
  tech_lead: string | null;
  roles_verified_at: string | null;
  health: { state: HealthState; reasons: string[] };
  issues: {
    total: number;
    open: number;
    awaiting_qa: number;
    closed: number;
    overdue: number;
    blocked: number;
    open_unassigned: number;
    open_no_estimate: number;
    open_no_date: number;
  };
  remaining_estimated_hours: number | null;
  estimate_coverage: number | null;
  spent_hours: number | null;
  last_activity: string | null;
  next_milestone: MilestoneRef | null;
  flag_counts: Record<string, number>;
  redmine_url?: string;
  delivery?: DeliveryDates;
  /** Direct subprojects the viewer may see. */
  subproject_ids?: number[];
  /** issues summed over this project and its visible subprojects; null without any. */
  rollup?: ProjectSummary["issues"] | null;
  rollup_remaining_estimated_hours?: number | null;
  rollup_estimate_coverage?: number | null;
}

export interface WorkloadConcern {
  person_id: number | null;
  person_name: string;
  period_start: string;
  period_end: string;
  available_hours: number | null;
  allocated_hours: number;
  remaining_estimated_hours: number | null;
  reason: string;
}

export interface Portfolio {
  projects_in_scope: number;
  by_health: Record<HealthState, number>;
  overdue_issues: number;
  blocked_issues: number;
  critical_blocked: number;
  open_issues: number;
  upcoming_milestones: MilestoneRef[];
  last_successful_sync: string | null;
  data_stale: boolean;
  projects: ProjectSummary[];
  workload_concerns: WorkloadConcern[];
  today: string;
}

export interface Flag {
  kind: string;
  severity: Severity;
  project_id: number;
  issue_id?: number;
  version_id?: number;
  title: string;
  evidence: string;
  action: string;
  project_name?: string;
  redmine_url?: string;
}

export interface Issue {
  id: number;
  project_id: number;
  subject: string;
  tracker: string;
  status: string;
  priority: string;
  is_closed: boolean;
  assignee_id: number | null;
  assignee_name: string | null;
  version_id: number | null;
  version_name: string | null;
  due_date: string | null;
  done_ratio: number | null;
  estimated_hours: number | null;
  spent_hours: number | null;
  updated_on: string;
  flags: string[];
  redmine_url?: string;
  awaiting_qa: boolean;
  // Fields whose value was set in the planner, and Redmine's value for each.
  planned?: PlanField[];
  redmine_values?: Partial<Record<PlanField, string | number | null>>;
}

// Fields that can be planned in the planner. Nothing is written to Redmine.
export type PlanField = "estimated_hours" | "due_date" | "assignee_id" | "version_id";

// planIssues applies one change to one or more issues; null goes back to
// the Redmine value.
export function planIssues(projectId: number, issueIds: number[], changes: Partial<Record<PlanField, string | number | null>>) {
  return api<void>(`/projects/${projectId}/plan`, { method: "POST", body: { issue_ids: issueIds, changes } });
}

export interface MilestoneInput {
  name: string;
  due_date: string | null;
  status?: "open" | "closed";
}

// Planner milestones are addressed by their planner ID, which is minus the
// version ID.
export function saveMilestone(projectId: number, versionId: number | null, body: MilestoneInput) {
  return versionId === null
    ? api<{ id: number }>(`/projects/${projectId}/planner-milestones`, { method: "POST", body })
    : api<{ id: number }>(`/projects/${projectId}/planner-milestones/${-versionId}`, { method: "PUT", body });
}

export function deleteMilestone(projectId: number, versionId: number) {
  return api<void>(`/projects/${projectId}/planner-milestones/${-versionId}`, { method: "DELETE" });
}

export interface Person {
  id: number;
  name: string;
}

export interface TeamMember {
  person_id: number;
  person_name: string | null;
  hours_per_week: number | null;
}

export interface Team {
  id: number;
  name: string;
  members: TeamMember[];
}

export interface Commitment {
  id: number;
  project_id: number;
  version_id: number | null;
  title: string;
  baseline_date: string | null;
  status: "proposed" | "approved" | "rejected" | "superseded" | "delivered";
  actual_date: string | null;
  reason: string;
  proposed_by: string;
  proposed_at: string;
  approved_by: string | null;
  approved_at: string | null;
  supersedes_id: number | null;
}

export interface Milestone extends MilestoneRef {
  remaining_estimated_hours: number | null;
  open_no_estimate: number;
  flags: Flag[];
  redmine_url?: string;
  commitment?: Commitment;
}

export interface SyncRun {
  id: number;
  kind: string;
  started_at: string;
  heartbeat_at: string;
  finished_at: string | null;
  outcome: string;
  counts: Record<string, Record<string, number>>;
  checkpoint_before: string | null;
  checkpoint_after: string | null;
  warnings: string[];
  error_summary: string | null;
}

export interface SyncStatusResponse {
  status: {
    runs: SyncRun[];
    last_success: SyncRun | null;
    checkpoint: string | null;
    latest_reconciliation: { project_id: number; api_total: number | null; stored_active: number; matches: boolean; note: string | null }[];
  };
  data_stale: boolean;
  latest_failed: boolean;
  stale_after: string;
  showing_previous: boolean;
}

export const FLAG_LABEL: Record<string, string> = {
  blocked_dependency: "Blocked dependency",
  overdue: "Overdue",
  stale_activity: "Stale activity",
  missing_owner: "No assignee",
  missing_estimate: "No estimate",
  missing_due_date: "No date",
  over_estimate: "Over estimate",
  milestone_overdue: "Milestone overdue",
  milestone_at_risk: "Milestone at risk",
  milestone_no_date: "Milestone without date",
  release_readiness: "Release readiness",
  forecast_after_baseline: "Forecast after baseline",
  capacity_risk: "Capacity risk",
  no_baseline: "No approved baseline",
};

export const HEALTH_LABEL: Record<HealthState, string> = {
  on_track: "On track",
  at_risk: "At risk",
  blocked: "Blocked",
  insufficient_data: "Not enough data",
};

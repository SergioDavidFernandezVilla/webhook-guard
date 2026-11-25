export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

export interface Charge {
  charge_id: string;
  state: string;
  amount_minor: number;
  currency: string;
  updated_at: string;
  last_event_id: string;
}

export interface LedgerEntry {
  id: number;
  charge_id: string;
  entry_type: string;
  amount_minor: number;
  currency: string;
  source_event_id: string;
  created_at: string;
}

export interface LateEvent {
  id: number;
  event_id: string;
  charge_id: string;
  reason: string;
  received_at: string;
  resolved_at: string | null;
}

export interface EventItem {
  id: number;
  event_id: string;
  type: string;
  charge_id: string;
  signature_timestamp: number;
  correlation_id: string;
  received_at: string;
  applied_at: string | null;
}

export interface EventDetail {
  event: EventItem & { payload: unknown };
  ledger: LedgerEntry | null;
  late: LateEvent | null;
}

export interface MetricPoint {
  name: string;
  window_start: string;
  window_seconds: number;
  count: number;
}

export interface OverviewReport {
  batch_id: string;
  created_at: string;
  checked: number;
  counts: Record<string, number>;
  has_diffs: boolean;
}

export interface Overview {
  generated_at: string;
  database: { ready: boolean; latency_ms: number };
  events: { total: number; applied: number; pending: number };
  charges: { total: number; by_state: Record<string, number> };
  ledger_entries: number;
  late_events: { pending: number; resolved: number };
  metric_totals: Record<string, number>;
  latest_report: OverviewReport | null;
}

export interface ChargeDetail {
  charge: Charge;
  ledger: LedgerEntry[];
  ledger_total: number;
  events: EventItem[];
  late_events: LateEvent[];
}

export interface ListCharges {
  items: Charge[];
  total: number;
  limit: number;
  offset: number;
}

export interface ListEvents {
  items: EventItem[];
  next_before_id: number | null;
}

export interface ListLateEvents {
  items: LateEvent[];
  total: number;
  limit: number;
  offset: number;
}

export interface ReportCounts {
  ok: number;
  missing_local: number;
  missing_remote: number;
  amount_mismatch: number;
  state_mismatch: number;
  currency_mismatch: number;
}

export interface DiffEntry {
  charge_id: string;
  category: string;
  expected?: unknown;
  actual?: unknown;
  source: string;
}

export interface Report {
  batch_id: string;
  checked: number;
  counts: ReportCounts;
  diffs: DiffEntry[];
  has_diffs?: boolean;
}

export interface LatestReport {
  report: { batch_id: string; created_at: string; report: Report } | null;
}

export interface StoredReport {
  id: number;
  batch_id: string;
  created_at: string;
  report: Report;
}

export interface LoadgenRun {
  id: number;
  route: string;
  mode: string;
  seed: number;
  target: string;
  events: number;
  concurrency: number;
  sent: number;
  accepted: number;
  duplicates: number;
  rejected: number;
  server_errors: number;
  other: number;
  transport_errors: number;
  p50_ms: number;
  p95_ms: number;
  p99_ms: number;
  avg_ms: number;
  duration_ms: number;
  created_at: string;
}

export interface Capabilities {
  demo_enabled: boolean;
}

export interface DemoStep {
  http_status: number;
  status?: string;
  error?: string;
}

export interface DemoResult {
  scenario: string;
  event_id: string;
  charge_id: string;
  steps: DemoStep[];
}

const BASE = "/api/v1";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      Accept: "application/json",
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
    },
    cache: "no-store",
  });

  if (!response.ok) {
    let code = "generic";
    let message = `HTTP ${response.status}`;
    try {
      const body = (await response.json()) as { error?: { code?: string; message?: string } };
      code = body.error?.code ?? code;
      message = body.error?.message ?? message;
    } catch {
      // The API always answers JSON; a parse failure stays a generic error.
    }
    throw new ApiError(response.status, code, message);
  }

  return (await response.json()) as T;
}

function query(params: Record<string, string | number | boolean | undefined | null>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === "") continue;
    search.set(key, String(value));
  }
  const encoded = search.toString();
  return encoded ? `?${encoded}` : "";
}

export function getOverview(signal?: AbortSignal): Promise<Overview> {
  return request<Overview>("/overview", { signal });
}

export function getMetrics(minutes: number, signal?: AbortSignal): Promise<{ items: MetricPoint[] }> {
  return request<{ items: MetricPoint[] }>(`/metrics${query({ minutes })}`, { signal });
}

export function listCharges(
  params: { state?: string; q?: string; limit?: number; offset?: number },
  signal?: AbortSignal,
): Promise<ListCharges> {
  return request<ListCharges>(`/charges${query(params)}`, { signal });
}

export function getCharge(chargeId: string, signal?: AbortSignal): Promise<ChargeDetail> {
  return request<ChargeDetail>(`/charges/${encodeURIComponent(chargeId)}`, { signal });
}

export function listEvents(
  params: { status?: string; type?: string; charge_id?: string; before_id?: number; limit?: number },
  signal?: AbortSignal,
): Promise<ListEvents> {
  return request<ListEvents>(`/events${query(params)}`, { signal });
}

export function getEvent(eventId: string, signal?: AbortSignal): Promise<EventDetail> {
  return request<EventDetail>(`/events/${encodeURIComponent(eventId)}`, { signal });
}

export function listLateEvents(
  params: { resolved?: boolean; charge_id?: string; limit?: number; offset?: number },
  signal?: AbortSignal,
): Promise<ListLateEvents> {
  return request<ListLateEvents>(`/late-events${query(params)}`, { signal });
}

export function getLatestReport(signal?: AbortSignal): Promise<LatestReport> {
  return request<LatestReport>("/reconciliation/latest", { signal });
}

export function listReports(limit = 20, signal?: AbortSignal): Promise<{ items: StoredReport[] }> {
  return request<{ items: StoredReport[] }>(`/reconciliation/reports${query({ limit })}`, { signal });
}

export function getLatestLoadgenRuns(signal?: AbortSignal): Promise<{ items: LoadgenRun[] }> {
  return request<{ items: LoadgenRun[] }>("/loadgen/latest", { signal });
}

export function listLoadgenRuns(limit = 20, signal?: AbortSignal): Promise<{ items: LoadgenRun[] }> {
  return request<{ items: LoadgenRun[] }>(`/loadgen/runs${query({ limit })}`, { signal });
}

export function getCapabilities(signal?: AbortSignal): Promise<Capabilities> {
  return request<Capabilities>("/capabilities", { signal });
}

export function postDemo(scenario: string, signal?: AbortSignal): Promise<DemoResult> {
  return request<DemoResult>("/demo/webhooks", {
    method: "POST",
    body: JSON.stringify({ scenario }),
    signal,
  });
}

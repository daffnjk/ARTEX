import { http } from "@/lib/api";

export interface ControlStatus {
  task_id: number;
  mode: "legacy" | "direct" | "goal" | "coverage";
  contract: {
    version: number;
    epoch: number;
    revoked: boolean;
    expires_at: string | null;
    anonymous_allowed: boolean;
    methods: { id: string; version: string }[];
    targets: { kind: string; value: string; ports?: number[] }[];
  };
  result: { phase: string; run: string; result: string; reason: string; cleanup: string; legacy_status: string };
  budgets: {
    dimension: string;
    measurement: string;
    limit: number;
    used: number;
    reserved: number;
    close_reserve: number;
    remaining: number;
    overrun: boolean;
  }[];
  executions: {
    id: string;
    run: string;
    effect: string;
    cleanup: string;
    cancel_requested: boolean;
    context_version: string;
    grant_expires: string;
  }[];
  evidence: {
    id: string;
    sha256: string;
    validity: string;
    complete: boolean;
    bytes: number;
    context_version: string;
    execution_id: string;
  }[];
  next: { execution_after: string; evidence_after: string };
  review: { advisory_records: number; trust_domain: string; independent: boolean };
}

const path = (task: string) => `/tasks/${encodeURIComponent(task)}/control`;
export const controlApi = {
  status: (task: string, executionAfter = "", evidenceAfter = "") =>
    http<ControlStatus>(
      `${path(task)}/status?${new URLSearchParams({ execution_after: executionAfter, evidence_after: evidenceAfter })}`,
    ),
  action: (task: string, operation: "cancel" | "close" | "retest", expectedEpoch: number, input: object) =>
    http<ControlStatus | { retest_ref: string; notice: string }>(`${path(task)}/${operation}`, {
      method: "POST",
      body: JSON.stringify({ ...input, expected_epoch: expectedEpoch }),
    }),
  approve: (task: string, expectedEpoch: number, contract: unknown) =>
    http<{ control_epoch: number }>(`${path(task)}/approve`, {
      method: "POST",
      body: JSON.stringify({ expected_epoch: expectedEpoch, contract }),
    }),
  revoke: (task: string, expectedEpoch: number) =>
    http<{ control_epoch: number }>(`${path(task)}/revoke`, {
      method: "POST",
      body: JSON.stringify({ expected_epoch: expectedEpoch, reason: "administrator requested revocation" }),
    }),
  wait: (task: string, execution: string) =>
    http<{ execution_id: string; outcome: { run: string }; feedback?: string }>(
      `${path(task)}/executions/${encodeURIComponent(execution)}?wait_ms=1000`,
    ),
};

export function controlResultLabel(status: ControlStatus): string {
  const labels: Record<string, string> = {
    complete: "已完成指定验收",
    partial: "部分结果",
    inconclusive: "结论未定",
    aborted: "已中止",
    accepting: "等待受控提议",
    draining: "正在收尾",
    closed: "已结案",
  };
  return labels[status.result.result || status.result.phase] ?? "状态未知";
}

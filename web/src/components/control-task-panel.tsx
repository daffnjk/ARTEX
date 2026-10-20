"use client";

import * as React from "react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { type ControlStatus, controlApi, controlResultLabel } from "@/lib/control";

export function ControlTaskPanel({
  taskId,
  status,
  refresh,
}: {
  taskId: string;
  status: ControlStatus | null;
  refresh: () => Promise<void>;
}) {
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState("");
  const [notice, setNotice] = React.useState("");
  const [contract, setContract] = React.useState("");
  const [executionAfter, setExecutionAfter] = React.useState("");
  const [evidenceAfter, setEvidenceAfter] = React.useState("");
  const [page, setPage] = React.useState<ControlStatus | null>(null);
  const authority = React.useRef("");
  React.useEffect(() => {
    const version = `${taskId}:${status?.contract.epoch}:${status?.result.phase}`;
    if (authority.current !== version) {
      authority.current = version;
      setPage(null);
      setExecutionAfter("");
      setEvidenceAfter("");
    }
  }, [taskId, status?.contract.epoch, status?.result.phase]);
  const matchingPage = page && String(page.task_id) === taskId ? page : null;
  const matchingStatus = status && String(status.task_id) === taskId ? status : null;
  const current = matchingPage ?? matchingStatus;
  async function run(operation?: () => Promise<unknown>) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      if (operation) await operation();
      setPage(null);
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  if (!current)
    return (
      <Alert>
        <AlertTitle>控制状态待核验</AlertTitle>
        <AlertDescription>请刷新或确认管理员身份；当前无法据此判断执行或验收成功。</AlertDescription>
      </Alert>
    );
  const epoch = current.contract.epoch;
  const controlled = current.mode !== "legacy";
  const uncertain = current.executions.some((e) => e.effect === "unknown" || e.cleanup === "unconfirmed");
  async function action(operation: "cancel" | "close" | "retest", input: object) {
    const result = await controlApi.action(taskId, operation, epoch, input);
    if ("retest_ref" in result) setNotice(`已批准一次复测：${result.retest_ref}。尚未提交执行。`);
  }
  async function next(execution: string, evidence: string) {
    setBusy(true);
    try {
      setPage(await controlApi.status(taskId, execution, evidence));
      setExecutionAfter(execution);
      setEvidenceAfter(evidence);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card id="control-state" data-testid="control-task-panel">
      <CardHeader>
        <CardTitle>受控验证 · {controlled ? controlResultLabel(current) : "旧任务未自动授权"}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-3 text-sm">
          <span>模式 {current.mode}</span>
          <span>
            合同 v{current.contract.version} / epoch {epoch}
          </span>
          <span>阶段 {current.result.phase}</span>
          <span>原因 {current.result.reason || "—"}</span>
          <span>cleanup: {current.result.cleanup}</span>
          <Button variant="outline" size="sm" disabled={busy} onClick={() => void run()}>
            刷新状态
          </Button>
        </div>
        {controlled && (
          <p className="text-sm text-muted-foreground">
            {current.result.result || "尚未结案"} 是指定任务的结果。兼容状态 {current.result.legacy_status}{" "}
            不代表全局覆盖或系统故障；清理确认仅覆盖本地资源。
          </p>
        )}
        {(uncertain ||
          current.result.cleanup === "unconfirmed" ||
          ["partial", "inconclusive", "aborted"].includes(current.result.result)) && (
          <Alert>
            <AlertTitle>保留未知与移交项</AlertTitle>
            <AlertDescription>
              部分结果、未知副作用或清理未确认均未视为全部成功。先收取原执行结果或交由授权人员对账。
            </AlertDescription>
          </Alert>
        )}
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        {notice && (
          <p role="status" className="break-all text-sm">
            {notice}
          </p>
        )}
        {controlled && (
          <>
            <p className="break-words text-xs text-muted-foreground">
              方法 {current.contract.methods.map((m) => `${m.id}/${m.version}`).join(", ")}；范围{" "}
              {current.contract.targets.map((t) => `${t.kind}:${t.value}`).join(", ")}；到期{" "}
              {current.contract.expires_at ?? "未知"}；撤销 {String(current.contract.revoked)}。Shell / browser / MCP /
              自定义命令保持关闭。
            </p>
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                disabled={busy || current.contract.revoked || current.result.phase !== "accepting"}
                onClick={() => void run(() => controlApi.revoke(taskId, epoch))}
              >
                撤销授权
              </Button>
              <Button
                variant="outline"
                disabled={busy || current.result.phase === "closed"}
                onClick={() => void run(() => action("close", { reason: "inconclusive" }))}
              >
                停止并保留部分结果
              </Button>
              {current.result.phase === "draining" && (
                <Button
                  variant="outline"
                  disabled={busy}
                  onClick={() => void run(() => action("close", { reason: "inconclusive", handoff: true }))}
                >
                  记录清理未确认并移交
                </Button>
              )}
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <caption className="pb-2 text-left font-medium">
                  预算（reserved 含未知消耗；收尾预留不可用于普通执行）
                </caption>
                <thead>
                  <tr>
                    <th>维度</th>
                    <th>计量</th>
                    <th>已用</th>
                    <th>预留</th>
                    <th>收尾预留</th>
                    <th>剩余</th>
                  </tr>
                </thead>
                <tbody>
                  {current.budgets.map((b) => (
                    <tr key={b.dimension}>
                      <td>
                        {b.dimension}
                        {b.overrun ? " · 超额已记录" : ""}
                      </td>
                      <td>{b.measurement}</td>
                      <td>{b.used}</td>
                      <td>{b.reserved}</td>
                      <td>{b.close_reserve}</td>
                      <td>{b.remaining}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <caption className="pb-2 text-left font-medium">执行 · 仅操作原 execution</caption>
                <thead>
                  <tr>
                    <th>ID / context</th>
                    <th>run</th>
                    <th>effect</th>
                    <th>cleanup</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {current.executions.map((e) => (
                    <tr key={e.id}>
                      <td className="max-w-64 break-all">
                        {e.id}
                        <br />
                        {e.context_version}
                      </td>
                      <td>{e.run}</td>
                      <td>{e.effect}</td>
                      <td>{e.cleanup}</td>
                      <td className="flex flex-wrap gap-1">
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={busy}
                          onClick={() =>
                            void run(async () => {
                              const result = await controlApi.wait(taskId, e.id);
                              setNotice(
                                result.feedback === "WAIT_EXPIRED"
                                  ? `仍在等待原执行 ${e.id}，未重新提交。`
                                  : `原执行状态：${result.outcome.run}`,
                              );
                            })
                          }
                        >
                          收取 / 等候
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={busy || e.cancel_requested || !["queued", "running"].includes(e.run)}
                          onClick={() => void run(() => action("cancel", { execution_id: e.id }))}
                        >
                          取消
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={
                            busy || current.result.phase !== "accepting" || !["succeeded", "cancelled"].includes(e.run)
                          }
                          onClick={() =>
                            void run(() => action("retest", { execution_id: e.id, reason: "independent_confirmation" }))
                          }
                        >
                          批准一次独立确认
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!current.executions.length && <p className="text-xs text-muted-foreground">无执行记录。</p>}
            </div>
            {current.next.execution_after && (
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => void next(current.next.execution_after, evidenceAfter)}
              >
                下一页执行
              </Button>
            )}
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <caption className="pb-2 text-left font-medium">宿主证据 · 不以模型摘要代替原文</caption>
                <thead>
                  <tr>
                    <th>引用 / SHA-256</th>
                    <th>有效性</th>
                    <th>完整</th>
                    <th>字节</th>
                  </tr>
                </thead>
                <tbody>
                  {current.evidence.map((e) => (
                    <tr key={e.id}>
                      <td className="max-w-96 break-all">
                        <a
                          className="underline"
                          href={`/api/tasks/${encodeURIComponent(taskId)}/control/observations/${encodeURIComponent(e.id)}`}
                        >
                          {e.id}
                        </a>
                        <br />
                        {e.sha256}
                      </td>
                      <td>{e.validity}</td>
                      <td>{String(e.complete)}</td>
                      <td>{e.bytes}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!current.evidence.length && (
                <p className="text-xs text-muted-foreground">暂无宿主观测；未测项仍未知。</p>
              )}
            </div>
            {current.next.evidence_after && (
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => void next(executionAfter, current.next.evidence_after)}
              >
                下一页证据
              </Button>
            )}
            <p className="text-xs text-muted-foreground">
              建议性审查 {current.review.advisory_records} 条；{current.review.trust_domain}
              ，不等同独立批准，也不授予执行权限。
            </p>
          </>
        )}
        <details>
          <summary className="cursor-pointer text-sm">人工审批合同（不会自动执行）</summary>
          <div className="mt-3 flex flex-col gap-2">
            <label htmlFor={`control-contract-${taskId}`} className="text-sm">
              填写完整合同 JSON；仅已验收的本地 HTTP GET 可执行，凭据不在状态页返回。
            </label>
            <Textarea
              id={`control-contract-${taskId}`}
              value={contract}
              onChange={(e) => setContract(e.target.value)}
              maxLength={32768}
              placeholder='{"mode":"direct","methods":[{"id":"http.get","version":"1"}],...}'
            />
            <Button
              variant="outline"
              disabled={busy || !contract.trim() || current.result.phase !== "accepting"}
              onClick={() => void run(() => controlApi.approve(taskId, epoch, JSON.parse(contract)))}
            >
              提交当前 epoch 的人工审批
            </Button>
          </div>
        </details>
      </CardContent>
    </Card>
  );
}

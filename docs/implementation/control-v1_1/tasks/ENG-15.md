# ENG-15 · 独立审查、评估身份和可追溯报告

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-10, ENG-14。

## 输入与实际代码入口
- `db/review_packet.go`
- `db/harness_store.go`
- `harness/evaluation.go`
- `report/`
- `server/`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- 只读 Reviewer 上下文
- 宿主归档的审查记录和结论门禁
- 报告每条结论的证据索引

## 实现步骤
1. Reviewer 输入冻结证据包和标准版本，不读取 Planner 自写 PASS 当成判断前提。
2. Reviewer 只提交评估建议；宿主分配评估身份/会话和记录，模型 JSON 中自填 reviewer 不赋权。
3. 对机械可检验结论由确定性 verifier 决定；复杂业务结论按方法规定转人工，不能把模型意见变正式权威。
4. 执行者不能覆盖审查产物；同模型另会话仅降低上下文耦合，不声称独立信任域。
5. 报告自动保留反证、未测、有效阴性条件和清理移交；结论与影响分层。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlWorkerCannotWriteReview` | `server` | Worker 无审查记录写权。 |
| `TestControlReviewBindsEvidenceVersion` | `db` | 评估绑定证据/标准版本，过期复用拒绝。 |
| `TestControlReviewerCannotAuthorize` | `server` | Reviewer 无法提高授权或生成许可。 |
| `TestControlReportEvidenceLinks` | `server` | 报告结论具可解析证据链接，未知项未被省略。 |

本票用例清单：`acceptance/ENG-15.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
可关闭可选 Reviewer 策略，但方法要求的人工审查和正式验证门禁仍保留。

## 完成证据与交接
填写 `reports/ENG-15/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。

# ENG-16 · 最小操作面：查看、撤销、等待和部分结果

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-07, ENG-14, ENG-15。

## 输入与实际代码入口
- `server/server.go`
- `server/engine.go`
- `web/package.json`
- `web/src/（路径先核查）`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- 现有 API 上增量控制状态查询与人工操作
- 任务详情展示授权版本/预算/执行/证据/清理
- 前端检查与后端身份测试

## 实现步骤
1. ENG-00 明确实际路由与页面路径；新增能力优先挂现有 task detail，不重建 dashboard。
2. 读接口返回 task contract/version、budget remaining/reserved、active executions、result/reason/cleanup 和证据摘要。
3. 撤销/取消/人工批准为认证的写操作，检查 task ownership 与 expected revision；不可复用模型工具身份。
4. UI 明确 partial/inconclusive、未知副作用和清理未确认；禁止用绿色 done 掩盖不完整。
5. 依照 web 锁文件和既有 package manager 跑现有 check/build；不为本工单顺手升级依赖，变更范围有限。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlStatusOwnership` | `server` | 无法读取别的任务许可、密钥引用或证据。 |
| `TestControlAdminActionRevision` | `server` | 旧版本人工审批/取消请求明确冲突。 |
| `TestControlStatusPartialVisible` | `server` | API 不丢失 partial/reason/cleanup 字段。 |

本票用例清单：`acceptance/ENG-16.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
前端 CI/build 与截图评审作为额外非 Go 证据，不能由这三条后端测试替代。

## 回退
UI 可回滚但应显示未知/不支持而非错误成功；后端撤销与控制规则不可回滚。

## 完成证据与交接
填写 `reports/ENG-16/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。

# ENG-03 · 实现类型化目标规范化与纯授权匹配

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-01, ENG-02。

## 输入与实际代码入口
- `guard/guard.go`
- `db/company_scope.go`
- `db/task_scope.go`
- `control/（新增）`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- control/target.go 与 scope.go（建议）
- 表驱动和模糊测试
- 明确支持/拒绝的目标格式表

## 实现步骤
1. 使用标准 URL/IP 类型；首版支持 exact_host、domain_subtree、cidr 的明确语义。未实现的 URL path/resource 约束拒绝，不退化为整主机。
2. 域名按标签比较，规范化端口、大小写、末尾点；IDN 使用仓库已有相关库时固定测试；歧义编码、userinfo、zone 与不支持 scheme 拒绝。
3. deny-wins，未命中 allow 为拒绝；混合 DNS 结果的策略首版保守全拒绝，不能悄悄只选可达一项。
4. 分别校验逻辑主机标识与每次实际连接 IP；同 IP 不等于同 host 授权。
5. 保持函数无网络副作用；DNS/实际连接的检查放 ENG-06。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlExactHostBoundary` | `control` | test.invalid 不匹配 x.test.invalid 或 eviltest.invalid。 |
| `TestControlDenyWins` | `control` | 更具体 allow 不能覆盖拒绝项。 |
| `TestControlAmbiguousTargetRejected` | `control` | 歧义 URL、端口、IP/编码输入被拒绝。 |
| `TestControlUnsupportedScopeKind` | `control` | 未实现约束 fail closed。 |

本票用例清单：`acceptance/ENG-03.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
仅允许回到更保守的拒绝策略；不能为减少误阻断改为 default allow。

## 完成证据与交接
填写 `reports/ENG-03/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。

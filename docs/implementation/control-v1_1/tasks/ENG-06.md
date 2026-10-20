# ENG-06 · 先实现一种受控本地 HTTP 适配器

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-03, ENG-05。

## 输入与实际代码入口
- `execution/（新增）`
- `guard/guard.go`
- `traffic/`
- `harness/scenarios/`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- 许可约束的 HTTP adapter
- 本地双服务器夹具（allowed/denied）
- 适配器能力清单和 deny 计数证据

## 实现步骤
1. 首版只运行本地隔离 HTTP/HTTPS 读型方法与合成数据，不接入漏洞利用载荷或通用 Shell。
2. adapter 获取宿主许可，限定 method、header 白名单、路径范围、凭据引用、超时、字节上限与请求次数。
3. 每一跳重定向重新检查；自定义受控 dial 对解析后 IP 检查且直接连接已验证地址，避免校验后再次解析；TLS/Host 对应原批准主机；环境代理不可成为绕路出口。
4. 开始每个实际请求/连接前核验许可与当前撤销状态；持续连接撤销由独立 supervisor 关闭；明确已发出请求不可撤回的界限。
5. 目标侧断言 denied server 命中数为零；不只断言函数返回 deny。DNS 变化用可注入 resolver，不访问公网上的 DNS。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlRedirectBoundary` | `execution` | 允许服务器跳向不允许服务器，后者计数为零。 |
| `TestControlPinnedDial` | `execution` | 解析变化不能在第二次隐式解析中逃出范围。 |
| `TestControlResponseLimit` | `execution` | 超大或不结束响应按上限停止并标记不完整。 |
| `TestControlGrantRevokedBeforeDial` | `execution` | 撤销后开始的新连接被拒绝。 |
| `TestControlProxyEnvCannotBypass` | `execution` | 环境代理不使请求绕开批准连接通道。 |

本票用例清单：`acceptance/ENG-06.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
本工单不证明任意 Shell/浏览器/MCP 的隔离。未验收通道必须在自主任务中保持不可用；扩展须另立工单。

## 回退
禁用 adapter/新任务执行，不回落标准无约束客户端。

## 完成证据与交接
填写 `reports/ENG-06/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。

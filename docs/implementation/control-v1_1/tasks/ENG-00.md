# ENG-00 · 建立可复核基线与非跳过测试门禁

状态：pending。本票是待实现规格，不是已完成声明。
依赖：无（先执行）。

## 输入与实际代码入口
- `go.mod`
- `go.sum`
- `db/testmain_test.go`
- `agent/testmain_test.go`
- `scripts/test-research-pg.sh`
- `scripts/test-harness-offline.sh`
- `.github/workflows/research-harness.yml`
- `docs/autopilot/STATE.json`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- baseline.json（版本、工作区差异、测试能力，不含密钥）
- entrypoints.md（外部效果、正式证据、终态写入清单）
- 可复用 JSON 测试收据与 required-test 清单

## 实现步骤
1. 先读仓库实际规则和 docs/autopilot 状态；不得把方案当作源代码现状。
2. 读取 go.mod 的实际版本；禁止修改最低版本适配本机。确认隔离 PG 及两种 DSN 都显式指向同一专用实例；不读取或输出生产配置。
3. 枚举现有 Test 函数和现有 runner 的过滤表达式；记录跑不到的新增测试风险。
4. 为后续测试 runner 接入 go test -json + required-tests 白名单；必测缺席、skip、package fail、非零退出均失败。
5. 保存开始时树哈希/提交与 dirty paths；已有失败与本轮新增失败分开，不把历史报告写成当前 PASS。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlCognitionDefaultAssembly` | `agent` | 默认认知工具与渲染存在；显式纪律的状态单独断言，不以 SetCognition 调用次数推断。 |
| `TestControlTaskExplorationIdentity` | `db` | task_id 与 exploration_id 不相等的夹具中，解析与授权使用正确身份。 |

本票用例清单：`acceptance/ENG-00.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
基线可记录 BLOCKED_ENV，但这不等于所需构建/PG 门禁通过。缺少相应环境时只做互不依赖的静态任务，不能合并后续生产改造。

## 回退
本工单不改变运行期业务逻辑；保留基线产物，不回滚用户原有改动。

## 完成证据与交接
填写 `reports/ENG-00/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。

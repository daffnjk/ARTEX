# 本次实施规格的源码核查摘录

仅静态读取上传包；未运行 ARTEX 代码/模型/网络测试。未知调用链以 ENG-00 复核为准。原包 SHA256：85877c685905d5766237f25e1ac718c46259a1e93b2440c4f59bf9c4884e4e54。

## C01 · go.mod:1–18

编译基线由 go.mod 决定，不降级。

文件 SHA256：`19d2ff9f7effa9f31c31b372bf2562f4086b8d554ea6700d5e4184371d719d62`

```text
   1 module github.com/Autumn-27/artex
   2 
   3 go 1.26.3
   4 
   5 require (
   6 	github.com/Autumn-27/norma v0.4.3
   7 	github.com/golang-jwt/jwt/v5 v5.3.1
   8 	github.com/google/uuid v1.6.0
   9 	github.com/jackc/pgx/v5 v5.10.0
  10 	github.com/klauspost/compress v1.18.6
  11 	github.com/lqqyt2423/go-mitmproxy v1.9.2
  12 	github.com/miekg/dns v1.1.72
  13 	github.com/projectdiscovery/dnsx v1.2.3
  14 	golang.org/x/crypto v0.55.0
  15 	golang.org/x/net v0.58.0
  16 	golang.org/x/text v0.41.0
  17 	modernc.org/sqlite v1.52.0
  18 )
```

## C02 · agent/planner.go:51–64

默认认知能力与显式纪律区分。

文件 SHA256：`c25e6d28c4b9b03e5fc551d119e15625ddf98ba46f5b33a5ddc2073ebc077763`

```text
  51 	// plans 非 nil 时启用持久化计划模式（research M1b.1）：Plan() 开头从权威存储
  52 	// hydrate（失败即停止本轮，绝不回退空内存），TodoWrite 先 CAS 落盘后确认，
  53 	// Options.Todos 置 nil、改用 plantodo.go 的持久化工具。nil = 内存模式原行为。
  54 	plans PlanPersister
  55 
  56 	// M4.2 消融开关（倒置存储：零值=全开，生产装配不调用 SetCognition）。
  57 	// cogToolsOff：不注册 M3 假设/实验工具；cogRenderOff：不渲染信念态与实验推荐。
  58 	cogToolsOff  bool
  59 	cogRenderOff bool
  60 
  61 	// cogDiscipline（M4.4 研究变体 E）：在 belief block 追加显式认知纪律
  62 	// 指令（证明前先假设→预注册实验→派发）。生产装配不设置。
  63 	cogDiscipline bool
  64 }
```

## C03 · db/task_plans.go:9–45

保留稳定 ID / RowID / CAS，补原位更新。

文件 SHA256：`a261a28d7b7377348914b997ac04f27470639c0c9be2f87229f43d3f19d56a69`

```text
   9 
  10 // task_plans 存储（research M1b.1）：planner 的任务计划从进程内 map 提升为权威
  11 // PostgreSQL 记录。revision 是 CAS 计数器：写入方必须携带它读取时的 revision，
  12 // 过期/并发写入得到显式冲突（绝不 last-write-wins）。
  13 //
  14 // 认知视角：计划是控制律的草稿。持久化给控制器状态可达性（Planner 重建/进程
  15 // 重启后不回空计划）；CAS 防止过期控制器（旧 Planner 实例）静默覆盖新状态。
  16 // 计划状态只是模型便签——任何验收/完成路径都不读这张表。
  17 
  18 // PlanStep 是计划中的一个步骤。ID 由宿主分配且跨保存稳定（按内容精确匹配保持，
  19 // 与展示顺序无关；改写文本=新步骤，SDK 整表替换接口无法表达"同一步骤改文案"，
  20 // 已知限制）。BasisNodeIDs 是该步骤派生自的探索节点（fact/意图），ExpectedOutput
  21 // 是预期产出的占位描述（M1b.1 只记录不绑定，绑定在 M2/M3）。
  22 type PlanStep struct {
  23 	ID             int64   `json:"id"`
  24 	Content        string  `json:"content"`
  25 	Status         string  `json:"status"`
  26 	BasisNodeIDs   []int64 `json:"basis_node_ids,omitempty"`
  27 	ExpectedOutput string  `json:"expected_output,omitempty"`
  28 }
  29 
  30 // TaskPlan 是一个探索（任务，二者 1:1）的当前计划。Revision 是 CAS 期望值：
  31 // Load 返回当前值（无记录返回 nil,nil —— 显式空计划，不臆造）；Save 用它做
  32 // 乐观并发控制，成功后返回携带新 Revision 的落盘结果。RowID 是存储行的身份
  33 // （BIGSERIAL 主键）：CAS 谓词同时校验 revision 与 RowID——行被删除后重建会
  34 // 获得新 RowID，持有旧行身份的迟到写入即使 revision 撞值（如都为 1）也会被
  35 // 拒绝（封死 delete→recreate 的 ABA 覆盖）。首次写入期望 (Revision 0, RowID 0)。
  36 type TaskPlan struct {
  37 	ExplorationID int64
  38 	RowID         int64
  39 	Revision      int64
  40 	Steps         []PlanStep
  41 	ModelNotes    string
  42 }
  43 
  44 // PlanConflictError 表示 CAS 冲突：HaveRevision 是数据库里的当前 revision；
  45 // HaveRevision==0 表示当前无行（计划已被删除/归档清除），期望值相对"行已消失"
```

## C04 · db/task_completion.go:9–46

复用现有完成入口，旧终态只有 done/failed/timeout。

文件 SHA256：`ff0a8f4cf9d0ba6d3b91ea9df41f6c4871d9b70e60e764b9f0217dec5b685b3f`

```text
   9 // M1b.3b（research）：共享事务性完成操作。
  10 //
  11 // M1b.3a 盘点的三个 done 写入点（planner-met 非 CAS、goalless 抽干、超时收尾）
  12 // 收敛到这一个入口：单事务内 (1) 锁任务行；(2) done 时权威复查"无 open goal"
  13 //（消除 HasOpenGoal 检查与落库之间的竞态）；(3) 验收重校验——当前冻结验收版本
  14 // 引用的每个 goal 节点必须已 met（harness/acceptance.go 的"最终完成必须原子性
  15 // 重校验"语义）；(4) 终态 CAS（先到终态者胜，迟到 met 不再覆盖 timeout——
  16 // 修复盘点 R6）；(5) completed_source 分记来源。
  17 // 注意：本操作不新增任何"自动放行"——它验证的是图内目标状态与当前验收版本，
  18 // 不消费 EvaluationReport（那要等 M2.3 的受控集成）；review 模式的工具级拦截
  19 // 语义不变。
  20 
  21 // ErrCompletionGoalOpen 表示仍有未达成目标，完成被拒绝（可恢复：达成后再试）。
  22 var ErrCompletionGoalOpen = errors.New("completion refused: open goals remain")
  23 
  24 // ErrCompletionAcceptanceUnmet 表示当前冻结验收版本引用的目标尚未全部 met。
  25 var ErrCompletionAcceptanceUnmet = errors.New("completion refused: current frozen acceptance references unmet goals")
  26 
  27 // terminalStatuses 与 tasks 状态 CHECK 一致。
  28 var terminalStatuses = "('done','failed','timeout')"
  29 
  30 // CompleteTaskGuarded 以单事务写入任务终态并分记来源。status 必须是终态之一；
  31 // source 是完成来源标识（model:prove_goal / system:goalless-drain /
  32 // system:timeout / human:... 等）。won=false 表示已有终态先到（保留原状态与
  33 // 原来源，非错误）。done 类完成附带图一致性与验收重校验。
  34 func (d *DB) CompleteTaskGuarded(id int64, status, source string) (won bool, err error) {
  35 	switch status {
  36 	case "done", "failed", "timeout":
  37 	default:
  38 		return false, fmt.Errorf("CompleteTaskGuarded: %q is not a terminal status", status)
  39 	}
  40 	tx, err := d.Begin()
  41 	if err != nil {
  42 		return false, err
  43 	}
  44 	defer tx.Rollback()
  45 	var explorationID int64
  46 	err = tx.QueryRow(`SELECT exploration_id FROM tasks WHERE id=$1 FOR UPDATE`, id).Scan(&explorationID)
```

## C05 · db/harness_store.go:11–39

已有宿主采集与追加存储，不能重复造同义库。

文件 SHA256：`684beee1752287d298a58fe37e6e83da209c3181a2b8bfa665dde6e222bec619`

```text
  11 )
  12 
  13 // M2.1（research）：评估 harness 的可信本地采集存储。
  14 //
  15 // 写侧仅宿主可用（StoreHarnessObservation/RecordEvaluation）——模型没有任何
  16 // 写观察的路径；读侧以任务范围构造 harness.Resolver（越任务引用在解析层就
  17 // 变为 evidence_unavailable，M1a 契约的调用方/任务访问边界）。两张表
  18 // append-only：触发器拒绝 UPDATE/DELETE，代码不提供任何修改路径。
  19 
  20 // ErrHarnessObservationConflict 表示同 id 但内容不同的重复写入（同 id 同内容
  21 // 为幂等成功）。
  22 var ErrHarnessObservationConflict = errors.New("harness observation id exists with different content")
  23 
  24 // StoreHarnessObservation 写入一条可信观察（宿主 API）。校验 harness 容量上限
  25 // 与 digest 一致性；同 id 同内容幂等成功，同 id 异内容显式冲突。
  26 func (d *DB) StoreHarnessObservation(o harness.Observation) error {
  27 	if o.ID == "" || o.TaskID <= 0 || o.ExperimentID == "" || o.ContextVersion == "" {
  28 		return fmt.Errorf("harness observation: id/task_id/experiment_id/context_version required")
  29 	}
  30 	if len(o.Payload) > harness.MaxArtifactBytes {
  31 		return fmt.Errorf("harness observation %s: payload exceeds %d bytes", o.ID, harness.MaxArtifactBytes)
  32 	}
  33 	digest := harness.Digest(o.Payload)
  34 	// 幂等判定必须比对完整身份（id 全局唯一）：同 id 且身份字段一致才幂等，
  35 	// 否则显式冲突——只比 digest 会把跨任务的撞 id 误判为幂等（实测发现）。
  36 	var (
  37 		exTask, exExp, exExec, exCtx, exSubj, exObj string
  38 		exDigest                                    string
  39 		exComplete                                  bool
```

## C06 · db/hypotheses.go:306–369

文本匹配更新路径需要版本化兼容和新事务入口。

文件 SHA256：`f8ad9b7afc605b3322cc3884128b0e8c5d53ddf9e4adcdf7348f0611a5cfa838`

```text
 306 type ExperimentOutcome struct {
 307 	Status        string  // observed | inconclusive
 308 	Discriminated int64   // 唯一匹配的假设 id（0=无）
 309 	Matches       []int64 // 期望匹配的假设集合
 310 	Reason        string
 311 }
 312 
 313 // RecordExperimentOutcome 记录实验观察并对齐假设状态：恰好一个假设的【声明
 314 // 期望】与观察文本匹配 → 该假设 supported、其余参试假设 refuted（事件留痕）；
 315 // 多匹配或全不匹配 → inconclusive，假设状态不动（绝不强拟合）。
 316 func (s *ExplorationStore) RecordExperimentOutcome(experimentID int64, observed string, factID int64) (ExperimentOutcome, error) {
 317 	if strings.TrimSpace(observed) == "" || len(observed) > MaxExpectationBytes*4 {
 318 		return ExperimentOutcome{}, fmt.Errorf("observed summary required (bounded)")
 319 	}
 320 	tx, err := s.db.Begin()
 321 	if err != nil {
 322 		return ExperimentOutcome{}, err
 323 	}
 324 	defer tx.Rollback()
 325 	var (
 326 		hypIDs []int64
 327 		expRaw []byte
 328 		status string
 329 	)
 330 	var hypRaw string
 331 	err = tx.QueryRow(`SELECT array_to_json(hypothesis_ids)::text, expectations, status FROM experiments
 332 		WHERE id=$1 AND exploration_id=$2 FOR UPDATE`, experimentID, s.expID).Scan(&hypRaw, &expRaw, &status)
 333 	_ = json.Unmarshal([]byte(hypRaw), &hypIDs)
 334 	if errors.Is(err, sql.ErrNoRows) {
 335 		return ExperimentOutcome{}, fmt.Errorf("experiment %d not found", experimentID)
 336 	}
 337 	if err != nil {
 338 		return ExperimentOutcome{}, err
 339 	}
 340 	if status != "dispatched" {
 341 		return ExperimentOutcome{}, fmt.Errorf("experiment %d is %q, not dispatched", experimentID, status)
 342 	}
 343 	expectations := map[int64]string{}
 344 	if err := json.Unmarshal(expRaw, &expectations); err != nil {
 345 		return ExperimentOutcome{}, err
 346 	}
 347 	// 匹配只认精确相等（审查 P1：双向 Contains 过宽——expected="200" 会匹配
 348 	// "3200"，短串巧合可造成假唯一匹配，而 refuted 不可逆）。要求观察与预注册
 349 	// 期望同粒度精确对写；匹配文本写入 outcome 供审计。
 350 	out := ExperimentOutcome{Status: "inconclusive"}
 351 	matched := map[int64]string{}
 352 	for hypID, expected := range expectations {
 353 		if strings.TrimSpace(observed) == strings.TrimSpace(expected) {
 354 			out.Matches = append(out.Matches, hypID)
 355 			matched[hypID] = expected
 356 		}
 357 	}
 358 	switch {
 359 	case len(out.Matches) == 1:
 360 		out.Status = "observed"
 361 		out.Discriminated = out.Matches[0]
 362 		out.Reason = "observation matches exactly one declared expectation"
 363 		for _, hypID := range hypIDs {
 364 			to := "refuted"
 365 			if hypID == out.Discriminated {
 366 				to = "supported"
 367 			}
 368 			res, err := tx.Exec(`UPDATE hypotheses SET status=$2, updated_at=now() WHERE id=$1 AND status='active'`, hypID, to)
 369 			if err != nil {
```

## C07 · db/llm_usage.go:64–80

现有事后用量账本是 best-effort，不等于硬预算。

文件 SHA256：`ae3c52a13a0640579f893e906e6c4d3164aff6594b7ae8c49079add58ee25bd6`

```text
  64 }
  65 
  66 // InsertLLMUsage appends one metering row. Best-effort: callers log and continue on
  67 // error (a lost metering row must never break the LLM call).
  68 func (d *DB) InsertLLMUsage(u *LLMUsage) error {
  69 	var expID any
  70 	if u.ExplorationID > 0 {
  71 		expID = u.ExplorationID
  72 	}
  73 	_, err := d.Exec(`
  74 INSERT INTO llm_usage(task_id, exploration_id, worker, model, profile_name, latency_ms, input_tokens, output_tokens, cache_read, cache_write, status)
  75 VALUES (NULLIF($1,''),$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10,$11)`,
  76 		u.TaskID, expID, u.Worker, u.Model, u.ProfileName,
  77 		u.LatencyMs, u.InputTokens, u.OutputTokens, u.CacheRead, u.CacheWrite, u.Status)
  78 	return err
  79 }
  80 
```

## C08 · db/db.go:59–78

迁移 advisory lock 固定在同一连接。

文件 SHA256：`05a8955ba9c4d592aa07cb8001b99a826cc55994650001103b3310d796c62e13`

```text
  59 // withSchemaMigrationLock pins the session-level lock to one checked-out
  60 // connection. Running pg_advisory_lock through *sql.DB is incorrect because a
  61 // later schema or unlock call may use a different pooled PostgreSQL session.
  62 func withSchemaMigrationLock(ctx context.Context, sqlDB *sql.DB, action func(*sql.Conn) error) (err error) {
  63 	conn, err := sqlDB.Conn(ctx)
  64 	if err != nil {
  65 		return err
  66 	}
  67 	defer conn.Close()
  68 	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, schemaMigrationLockKey); err != nil {
  69 		return fmt.Errorf("advisory lock: %w", err)
  70 	}
  71 	defer func() {
  72 		if _, unlockErr := conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, schemaMigrationLockKey); unlockErr != nil && err == nil {
  73 			err = fmt.Errorf("advisory unlock: %w", unlockErr)
  74 		}
  75 	}()
  76 	return action(conn)
  77 }
  78 
```

## C09 · db/db.go:89–104

DSN 来源优先级与 database/sql 包装。

文件 SHA256：`05a8955ba9c4d592aa07cb8001b99a826cc55994650001103b3310d796c62e13`

```text
  89 // DSN resolves the PostgreSQL connection string and reports where it came from.
  90 // Precedence: env ARTEX_PG_DSN > config file (config.json). There is no
  91 // built-in default — it errors if neither source is configured.
  92 func DSN() (dsn, source string, err error) {
  93 	return config.PostgresDSN()
  94 }
  95 
  96 // DB wraps the shared *sql.DB. PG handles its own connection pool + concurrency
  97 // (MVCC), so unlike the old SQLite store there is no process-wide write mutex.
  98 type DB struct{ *sql.DB }
  99 
 100 // ensureDatabase connects to the postgres system database and creates the target
 101 // database if it does not exist. dsn must be a postgres:// URL.
 102 func ensureDatabase(dsn string) error {
 103 	u, err := url.Parse(dsn)
 104 	if err != nil {
```

## C10 · db/testmain_test.go:10–34

普通测试会按实际配置连接数据库；先做隔离。

文件 SHA256：`64331ed5a9428f788b378ea01583b1ad8ca85e9935ec98cca78114d499f3d089`

```text
  10 
  11 // TestMain acquires a PostgreSQL advisory lock (7337741002) for the entire
  12 // db test suite. Packages agent and server hold the same lock, so parallel
  13 // `go test ./...` runs serialize across packages on the shared dev DB and
  14 // avoid cross-package cleanup races (e.g. DELETE FROM assets WHERE id > X
  15 // from one package deleting assets created by another).
  16 func TestMain(m *testing.M) {
  17 	dsn, _, err := DSN()
  18 	if err != nil {
  19 		// No DB configured — tests that need PG will skip themselves.
  20 		os.Exit(m.Run())
  21 	}
  22 	conn, err := sql.Open("pgx", dsn)
  23 	if err != nil || conn.Ping() != nil {
  24 		os.Exit(m.Run())
  25 	}
  26 	defer conn.Close()
  27 	if _, err := conn.Exec(`SELECT pg_advisory_lock(7337741002)`); err != nil {
  28 		os.Exit(m.Run())
  29 	}
  30 	defer conn.Exec(`SELECT pg_advisory_unlock(7337741002)`) //nolint:errcheck
  31 	os.Exit(m.Run())
  32 }
```

## C11 · scripts/test-harness-offline.sh:1–18

离线 runner 显式只验证 Go1.23 标准库 harness 子集。

文件 SHA256：`e9e218e8df7b8f6eb807363f19def58d7c372e6332874a33c4e095c5480b4077`

```text
   1 #!/bin/sh
   2 # Test ONLY the new standard-library harness/CLI in a disposable Go 1.23 module.
   3 # This does not downgrade ARTEX's go.mod or claim that the whole app was tested.
   4 set -eu
   5 root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
   6 tmp=$(mktemp -d)
   7 trap 'rm -rf "$tmp"' EXIT HUP INT TERM
   8 mkdir -p "$tmp/cmd"
   9 cp -R "$root/harness" "$tmp/harness"
  10 cp -R "$root/cmd/harness-eval" "$tmp/cmd/harness-eval"
  11 printf 'module github.com/Autumn-27/artex\n\ngo 1.23.0\n' > "$tmp/go.mod"
  12 export GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off
  13 cd "$tmp"
  14 go version
  15 go test -race -count=1 -v -coverprofile=coverage.out ./harness ./cmd/harness-eval
  16 go vet ./harness ./cmd/harness-eval
  17 go build -o harness-eval ./cmd/harness-eval
  18 status=0
```

## C12 · scripts/test-research-pg.sh:1–22

专用 PG runner 的隔离入口；新增测试需被实际选中。

文件 SHA256：`e9e34e672cca215def88ab6c3be9ca016e84c44adc9156ff02e3f1489357cfe4`

```text
   1 #!/bin/sh
   2 # M1b.1 专用隔离 PostgreSQL 测试 runner（research）。
   3 #
   4 # 用法: ARTEX_TEST_PG_DSN='postgres://user@localhost:5432/artex_research_test' \
   5 #       sh scripts/test-research-pg.sh
   6 #
   7 # 硬性要求（拒绝而非跳过）：
   8 #   1. DSN 必须显式提供，且不得携带查询参数（pgx 会应用 ?host=/?database=
   9 #      覆盖项，只看 authority 的守卫可被绕过）；
  10 #   2. 主机必须是 localhost/127.0.0.1/::1（本地隔离库，绝不连远程/生产）；
  11 #   3. 库名必须以 artex_research_test 开头（专用可重置测试库）。
  12 # 满足后运行 task_plans 的完整 CAS/恢复/删除/归档矩阵（该套件本身也有同款守卫），
  13 # 并以同一 DSN 激活 db 包既有的归档 PG 回归——回归若因库不可达而 SKIP 同样判失败
  14 #（专用 runner 里不允许静默跳过）。绝不使用生产 DSN。
  15 set -eu
  16 
  17 root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
  18 DSN=${ARTEX_TEST_PG_DSN:-}
  19 
  20 fail() { echo "test-research-pg.sh: $*" >&2; exit 2; }
  21 
  22 [ -n "$DSN" ] || fail "ARTEX_TEST_PG_DSN is required (e.g. postgres://user@localhost:5432/artex_research_test)"
```

## C13 · server/task_llm.go:222–239

模型 runtime 包装入口，应覆盖 retry/failover 的实际尝试。

文件 SHA256：`9f9671b9728d3150630ce4dc5d3da7893362d27e505f5d77dcf0eb13ecc351c7`

```text
 222 func (r *taskLLMRuntime) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
 223 	ctx = llmrec.WithTaskID(ctx, r.taskID)
 224 	return streamTaskLLM(ctx, r.taskID, req, r.streamHooks())
 225 }
 226 
 227 // Complete is the non-streaming counterpart of Stream. A non-streaming call is
 228 // atomic — it never delivers partial output — so every failure is safe to retry
 229 // on the same provider or fail over to the next profile without risking
 230 // duplicated model output or tool execution (the "committed" bookkeeping the
 231 // streaming path needs is unnecessary here).
 232 func (r *taskLLMRuntime) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
 233 	ctx = llmrec.WithTaskID(ctx, r.taskID)
 234 	return completeTaskLLM(ctx, r.taskID, req, r.streamHooks())
 235 }
 236 
 237 func completeTaskLLM(ctx context.Context, taskID string, req llm.CompletionRequest, hooks taskLLMStreamHooks) (llm.Message, string, llm.Usage, error) {
 238 	for {
 239 		selection, err := hooks.current()
```

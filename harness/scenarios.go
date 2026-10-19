package harness

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// M0.1（research）：版本化本地场景/真值登记 + 可执行回放。
//
// 每个场景是一份不可变（按 id+version 标识）的离线评估夹具：冻结验收规格、
// 一个确定性验收器配置、按步骤排列的请求与观察材料、以及来自构造性真值的
// 预期判定。回放把每步请求送进与生产同一套评估内核（M1a.1 的可信绑定路径），
// 断言实际结论与真值标签一致——这既是 M0 能力基线的种子登记，也是内核行为
// 的回归基线。场景不接触目标、不调用模型、不授予任何运行时权限。

// ScenarioClasses 是登记册覆盖的样本类（交接方案 M0 优先清单）。
var ScenarioClasses = []string{
	"valid-session",               // 会话有效：完整证据满足冻结验收
	"invalid-value",               // 会话无效：捕获值与命题相反
	"missing-evidence",            // 缺证据：引用的观察不存在
	"contradictory-observation",   // 矛盾观察：两份材料互斥
	"condition-version-change",    // 条件版本改变：观察与请求上下文版本不一致
	"tool-error-or-cancel",        // 工具错误/取消/超时：观察不完整或评估被取消
	"duplicate-observation",       // 重复观察：同一观察 id 被引用两次
	"oversized-output",            // 异常大输出：材料超过容量上限
	"cross-task-reference",        // 跨任务引用证据
	"acceptance-binding-mismatch", // 请求与冻结验收规格不匹配
	"tampered-evidence",           // 材料篡改：sha256 与实际字节不符
	"verifier-indefinite",         // 验收器无法判定（unknown → review）
	"verifier-failure",            // 验收器内部错误/坏判定（fail closed）
	"restart-resume",              // 运行重启：中断后凭权威材料恢复并完成判定
}

// ScenarioControls 登记该场景运行所需的环境/预算边界（M0.2 预注册将扩展同一
// 格式加入模型与 token 预算）。离线评估场景不需要模型、不接触目标。
type ScenarioControls struct {
	Model      string `json:"model"`
	ToolBudget string `json:"tool_budget"`
	Target     string `json:"target"`
}

// ScenarioVerifier 描述一个由宿主登记的确定性验收器（回放时构造；模型不可
// 注册验收器）。Kind 决定判定逻辑，Expect 是其参数：
//
//	payload-equals       全部材料字节一致且等于 Expect(base64)→valid/supported；
//	                     一致但不等于→valid/refuted；材料互斥→unknown/inconclusive。
//	payload-contains     材料（拼接后）包含 Expect 子串→supported，否则 refuted。
//	verifier-unknown     恒 unknown/inconclusive（验收器无法判定）。
//	verifier-error       恒返回错误（内核 fail-closed）。
//	verifier-bad-verdict 返回非法判定（内核拒绝）。
type ScenarioVerifier struct {
	Kind   string `json:"kind"`
	Expect string `json:"expect,omitempty"`
}

// StepExpect 是该步的构造性真值：预期结论/处置与首个问题码（来自评估内核
// 契约，evaluation.go 的路径映射）。
type StepExpect struct {
	Conclusion  Conclusion  `json:"conclusion"`
	Disposition Disposition `json:"disposition"`
	IssueCode   string      `json:"issue_code,omitempty"`
}

// ScenarioStep 一步评估：请求 + 本步可见的观察材料（重启/恢复场景靠多步与
// 不同材料集合表达）。CancelContext 为 true 时以已取消的上下文回放（取消类）。
// SyntheticSizes 让超大材料在回放时确定性生成（'A' 填充，避免 MB 级 JSON 入
// 库）；对应证据引用的 sha256 写 "computed" 由回放补齐，写死值则保留原样
// （供篡改类场景使用）。
type ScenarioStep struct {
	Name           string            `json:"name"`
	Request        EvaluationRequest `json:"request"`
	Observations   []Observation     `json:"observations"`
	SyntheticSizes map[string]int    `json:"synthetic_sizes,omitempty"`
	CancelContext  bool              `json:"cancel_context,omitempty"`
	Expect         StepExpect        `json:"expect"`
}

// ScenarioTruth 记录真值来源与适用条件——标签不是模型自评，而是作者按内核
// 契约对构造材料的预期。
type ScenarioTruth struct {
	Source    string `json:"source"`
	Condition string `json:"condition"`
}

// Scenario 是一份登记的场景（id+version 唯一且不可变）。
type Scenario struct {
	SchemaVersion  string           `json:"schema_version"`
	ID             string           `json:"id"`
	Version        string           `json:"version"`
	SampleClass    string           `json:"sample_class"`
	Description    string           `json:"description"`
	AllowedActions []string         `json:"allowed_actions"`
	StopConditions []string         `json:"stop_conditions"`
	Controls       ScenarioControls `json:"controls"`
	Truth          ScenarioTruth    `json:"truth"`
	Acceptance     AcceptanceSpec   `json:"acceptance"`
	Verifier       ScenarioVerifier `json:"verifier"`
	Steps          []ScenarioStep   `json:"steps"`
}

//go:embed scenarios/*.json
var scenarioFS embed.FS

// LoadScenarios 载入并校验全部内嵌场景（结构完整性：版本、类目、步骤与枚举
// 合法；不校验内核合法性——部分场景故意携带非法请求，那是它们的真值点）。
func LoadScenarios() ([]Scenario, error) {
	entries, err := fs.ReadDir(scenarioFS, "scenarios")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]Scenario, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		raw, err := scenarioFS.ReadFile("scenarios/" + ent.Name())
		if err != nil {
			return nil, err
		}
		var s Scenario
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&s); err != nil {
			return nil, fmt.Errorf("scenario %s: %w", ent.Name(), err)
		}
		if err := s.validate(); err != nil {
			return nil, fmt.Errorf("scenario %s: %w", ent.Name(), err)
		}
		key := s.ID + "@" + s.Version
		if seen[key] {
			return nil, fmt.Errorf("duplicate scenario id+version %s", key)
		}
		seen[key] = true
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s Scenario) validate() error {
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version")
	}
	if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.Version) == "" {
		return fmt.Errorf("id and version required")
	}
	known := false
	for _, c := range ScenarioClasses {
		if c == s.SampleClass {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("unknown sample_class %q", s.SampleClass)
	}
	if err := s.Acceptance.Validate(); err != nil {
		return fmt.Errorf("acceptance: %w", err)
	}
	switch s.Verifier.Kind {
	case "payload-equals", "payload-contains", "verifier-unknown", "verifier-error", "verifier-bad-verdict":
	default:
		return fmt.Errorf("unknown verifier kind %q", s.Verifier.Kind)
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("at least one step required")
	}
	for i, st := range s.Steps {
		if st.Expect.Conclusion == "" || st.Expect.Disposition == "" {
			return fmt.Errorf("step %d: expected conclusion/disposition required", i)
		}
		switch st.Expect.Conclusion {
		case Inconclusive, Supported, Refuted:
		default:
			return fmt.Errorf("step %d: bad expected conclusion %q", i, st.Expect.Conclusion)
		}
		switch st.Expect.Disposition {
		case Review, Confirm, NotSupported, Retry:
		default:
			return fmt.Errorf("step %d: bad expected disposition %q", i, st.Expect.Disposition)
		}
	}
	return nil
}

// buildVerifier 按场景配置构造确定性验收器（宿主注册路径，与请求无关）。
func (v ScenarioVerifier) build() Verifier {
	switch v.Kind {
	case "payload-equals":
		want, _ := base64.StdEncoding.DecodeString(v.Expect)
		return func(_ context.Context, _ EvaluationRequest, obs []Observation) (Verdict, error) {
			ids := make([]string, 0, len(obs))
			var first []byte
			same := true
			for _, o := range obs {
				ids = append(ids, o.ID)
				if first == nil {
					first = o.Payload
					continue
				}
				if !bytes.Equal(first, o.Payload) {
					same = false
				}
			}
			switch {
			case !same:
				return Verdict{Validity: ValidityUnknown, Conclusion: Inconclusive,
					EvidenceIDs: ids, Reason: "scenario verifier: observations contradict each other"}, nil
			case bytes.Equal(first, want):
				return Verdict{Validity: ValidityValid, Conclusion: Supported,
					EvidenceIDs: ids, Reason: "scenario verifier: captured payload equals the expected constant"}, nil
			default:
				return Verdict{Validity: ValidityValid, Conclusion: Refuted,
					EvidenceIDs: ids, Reason: "scenario verifier: captured payload differs from the expected constant"}, nil
			}
		}
	case "payload-contains":
		return func(_ context.Context, _ EvaluationRequest, obs []Observation) (Verdict, error) {
			ids := make([]string, 0, len(obs))
			var all []byte
			for _, o := range obs {
				ids = append(ids, o.ID)
				all = append(all, o.Payload...)
			}
			if strings.Contains(string(all), v.Expect) {
				return Verdict{Validity: ValidityValid, Conclusion: Supported,
					EvidenceIDs: ids, Reason: "scenario verifier: payload contains the expected marker"}, nil
			}
			return Verdict{Validity: ValidityValid, Conclusion: Refuted,
				EvidenceIDs: ids, Reason: "scenario verifier: payload lacks the expected marker"}, nil
		}
	case "verifier-unknown":
		return func(_ context.Context, _ EvaluationRequest, obs []Observation) (Verdict, error) {
			ids := make([]string, 0, len(obs))
			for _, o := range obs {
				ids = append(ids, o.ID)
			}
			return Verdict{Validity: ValidityUnknown, Conclusion: Inconclusive,
				EvidenceIDs: ids, Reason: "scenario verifier: cannot decide from available evidence"}, nil
		}
	case "verifier-error":
		return func(context.Context, EvaluationRequest, []Observation) (Verdict, error) {
			return Verdict{}, fmt.Errorf("scenario verifier: simulated internal failure")
		}
	case "verifier-bad-verdict":
		return func(context.Context, EvaluationRequest, []Observation) (Verdict, error) {
			return Verdict{Validity: Validity("bogus"), Conclusion: Supported,
				Reason: "scenario verifier: intentionally malformed verdict"}, nil
		}
	}
	return nil
}

type stepResolver map[string]Observation

func (r stepResolver) Resolve(_ context.Context, id string) (Observation, error) {
	o, ok := r[id]
	if !ok {
		return Observation{}, fmt.Errorf("observation %s not found", id)
	}
	return o, nil
}

// ReplayStep 以生产同一套内核回放一步，返回实际报告。合成材料在此展开并
// 补齐 "computed" 摘要。
func (s Scenario) ReplayStep(step ScenarioStep) EvaluationReport {
	source, err := NewFrozenAcceptanceSource([]AcceptanceSpec{s.Acceptance})
	if err != nil {
		return EvaluationReport{Validity: ValidityUnknown, Conclusion: Inconclusive,
			Disposition: Review, Reason: "scenario acceptance invalid: " + err.Error()}
	}
	registry := map[Criterion]Verifier{{ID: s.Acceptance.Criterion.ID, Version: s.Acceptance.Criterion.Version}: s.Verifier.build()}
	store := stepResolver{}
	for _, o := range step.Observations {
		store[o.ID] = o
	}
	for id, size := range step.SyntheticSizes {
		o := store[id]
		o.Payload = bytes.Repeat([]byte("A"), size)
		store[id] = o
	}
	req := step.Request
	req.Evidence = append([]EvidenceRef(nil), req.Evidence...)
	for i, ref := range req.Evidence {
		if ref.ObservationID != "" && ref.SHA256 == "computed" {
			if o, ok := store[ref.ObservationID]; ok {
				req.Evidence[i].SHA256 = Digest(o.Payload)
			}
		}
	}
	ctx := context.Background()
	if step.CancelContext {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		ctx = cancelled
	}
	ev := NewEvaluatorWithAcceptance(store, registry, source)
	return ev.Evaluate(ctx, req)
}

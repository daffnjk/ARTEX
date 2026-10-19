package harness

import (
	"context"
	"fmt"
	"strings"
)

// M2.2（research）：首个领域验收器——前提标记验证。
//
// 边界（M1a 契约）：领域规则只证明【实验前提/对照有效性】，绝不自动证明
// 整个漏洞或总目标。marker 由宿主在注册时以受信配置提供——绝不来自模型输入
// 或评估请求。判定：全部材料含标记 → valid/supported（前提成立）；全部不含 →
// valid/refuted（前提被反证，仅此前提）；混合 → unknown/inconclusive（矛盾
// 观察不裁决）。

// NewPremiseMarkerVerifier 构造前提标记验收器（宿主注册路径）。
func NewPremiseMarkerVerifier(marker string) (Verifier, error) {
	if strings.TrimSpace(marker) == "" {
		return nil, fmt.Errorf("premise marker verifier: marker must be host-provided and non-empty")
	}
	return func(_ context.Context, _ EvaluationRequest, obs []Observation) (Verdict, error) {
		if len(obs) == 0 {
			return Verdict{}, fmt.Errorf("premise marker verifier: no observations")
		}
		ids := make([]string, 0, len(obs))
		with, without := 0, 0
		for _, o := range obs {
			ids = append(ids, o.ID)
			if strings.Contains(string(o.Payload), marker) {
				with++
			} else {
				without++
			}
		}
		switch {
		case without == 0:
			return Verdict{Validity: ValidityValid, Conclusion: Supported, EvidenceIDs: ids,
				Reason: "premise verified: every captured observation contains the host-defined marker"}, nil
		case with == 0:
			return Verdict{Validity: ValidityValid, Conclusion: Refuted, EvidenceIDs: ids,
				Reason: "premise refuted: no captured observation contains the host-defined marker (this premise only)"}, nil
		default:
			return Verdict{Validity: ValidityUnknown, Conclusion: Inconclusive, EvidenceIDs: ids,
				Reason: "premise undecided: captures disagree about the host-defined marker"}, nil
		}
	}, nil
}

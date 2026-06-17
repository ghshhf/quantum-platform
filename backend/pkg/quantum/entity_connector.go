package quantum

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ghshhf/quantum-platform/backend/pkg/connector"
)

// ConnectorEntity 把一个外部 API / 服务（connector.ConnectorSpec）
// 包装成一个可对话的"量子节点"。
//
// 这体现了"只要产生数据交互，就是 API，就是一个可对话的主体"：
//  - 它的 Actions 就像它会"做的事情"
//  - 用户不需要知道具体哪个 action、参数怎么填
//  - Bridge 调用它时，它自己做"意图 → action + 参数"的映射
//
// 当前实现用启发式（action 描述/关键词匹配）来做意图识别，
// 后续可以升级为"调用 LLM 做意图分类"。
type ConnectorEntity struct {
	spec      *connector.ConnectorSpec
	executor  connectorExecutor
	cred      *connector.Credential
}

// connectorExecutor 是 connector 执行器的简化接口（方便测试）
type connectorExecutor interface {
	Execute(ctx context.Context, spec *connector.ConnectorSpec,
		action string, params map[string]any, cred *connector.Credential) (*connector.ExecuteResult, error)
}

// realExecutor 是对 connector.Runtime 的薄封装
type realExecutor struct {
	rt *connector.Runtime
}

func (r *realExecutor) Execute(ctx context.Context, spec *connector.ConnectorSpec,
	action string, params map[string]any, cred *connector.Credential) (*connector.ExecuteResult, error) {
	// 临时注册（如果还没注册过），再执行
	if _, ok := r.rt.Registry().Get(spec.Name); !ok {
		r.rt.Register(spec)
	}
	return r.rt.Execute(ctx, spec.Name, action, params, cred)
}

// NewConnectorEntity 从 ConnectorSpec 创建一个 ConnectorEntity
func NewConnectorEntity(rt *connector.Runtime, spec *connector.ConnectorSpec, cred *connector.Credential) *ConnectorEntity {
	return &ConnectorEntity{
		spec:     spec,
		executor: &realExecutor{rt: rt},
		cred:     cred,
	}
}

func (c *ConnectorEntity) Profile() EntityProfile {
	// 从 actions 中提取关键词（action.label/description 中的词）
	keywords := make([]string, 0, len(c.spec.Actions)*3)
	for _, a := range c.spec.Actions {
		keywords = append(keywords, a.Action)
		if a.Label != "" {
			// 提取 label 中的词
			words := tokenize(a.Label)
			seen := make(map[string]struct{})
			for _, w := range words {
				if _, dup := seen[w]; !dup && len(w) > 0 {
					keywords = append(keywords, w)
					seen[w] = struct{}{}
				}
			}
		}
	}
	return EntityProfile{
		Name:        c.spec.Name,
		Label:       firstNonEmpty(c.spec.Label, c.spec.Name),
		Kind:        KindConnector,
		Description: firstNonEmpty(c.spec.Description, "一个外部 API 服务"),
		Keywords:    head(uniq(keywords), 30),
		Metadata: map[string]string{
			"type":     string(c.spec.Type),
			"base_url": c.spec.BaseURL,
			"actions":  iToStr(len(c.spec.Actions)),
		},
		CreatedAt: time.Now(),
	}
}

// Match 用问题和 connector 的 action 标签/关键词做匹配
func (c *ConnectorEntity) Match(question string) float64 {
	profile := c.Profile()
	qTokens := make(map[string]struct{})
	for _, t := range tokenize(question) {
		qTokens[t] = struct{}{}
	}
	if len(qTokens) == 0 {
		return 0
	}
	// 额外：对 connector 的名称/label 做整体子串匹配
	nameLower := strings.ToLower(profile.Name + " " + profile.Label + " " + profile.Description)
	hit := 0
	seen := make(map[string]struct{})
	for _, k := range profile.Keywords {
		if _, ok := qTokens[k]; ok {
			if _, dup := seen[k]; !dup {
				hit++
				seen[k] = struct{}{}
			}
		}
	}
	// 名字/label 子串加分
	nameHitBonus := 0.0
	for t := range qTokens {
		if len(t) >= 2 && strings.Contains(nameLower, t) {
			nameHitBonus += 0.05
		}
	}
	raw := float64(hit)/float64(len(qTokens))*0.7 + nameHitBonus
	if hit > 0 {
		raw += 0.1
	}
	return clamp01(raw)
}

// Execute 从问题里挑最合适的 action，并尽可能从问题里提取参数。
//
// 当前实现用"action.Label/Action 名称与问题的相似度"来挑 action。
// 参数部分：对每个必填参数，尝试从问题里抽取值。
//
// 返回格式：一个 EntityFragment，包含该 action 返回的 JSON 数据的格式化摘要。
func (c *ConnectorEntity) Execute(ctx context.Context, query EntityQuery) EntityResult {
	start := time.Now()
	profile := c.Profile()

	if len(c.spec.Actions) == 0 {
		return EntityResult{
			Profile:   profile,
			LatencyMs: time.Since(start).Milliseconds(),
			Error:     "该 API 服务没有注册任何 action",
		}
	}

	// 步骤 1：挑一个最相关的 action
	pickedIdx, pickedScore := c.pickAction(query.Question)
	if pickedScore < 0.05 {
		return EntityResult{
			Profile: profile,
			Fragments: []EntityFragment{
				{
					EntityName: profile.Name,
					Content:    fmt.Sprintf("（%s：从问题中无法判断应调用哪个 action，建议更明确地提到具体操作，如 '查设备列表'、'登录' 等）", profile.Label),
					Confidence: 0.2,
				},
			},
			LatencyMs: time.Since(start).Milliseconds(),
		}
	}

	action := &c.spec.Actions[pickedIdx]

	// 步骤 2：从问题中提取参数
	params := c.extractParams(action, query.Question)

	// 步骤 3：执行
	result, err := c.executor.Execute(ctx, c.spec, action.Action, params, c.cred)
	if err != nil {
		return EntityResult{
			Profile:   profile,
			LatencyMs: time.Since(start).Milliseconds(),
			Error:     err.Error(),
		}
	}

	// 步骤 4：把 ExecuteResult 包装成 EntityFragment
	frag := EntityFragment{
		EntityName: profile.Name,
		SourceRef:  action.Action,
		Confidence: clamp01(pickedScore),
		Raw:        result,
	}
	if result.Error != "" {
		frag.Content = "调用 " + action.Label + " 失败: " + result.Error
	} else if result.Raw != "" {
		// 尝试把 JSON 格式化成人能读的摘要
		frag.Content = formatAsHumanReadable(result.Raw, action.Action)
	} else {
		frag.Content = "调用 " + action.Label + " 成功（无返回数据）"
	}

	return EntityResult{
		Profile:   profile,
		Fragments: []EntityFragment{frag},
		LatencyMs: time.Since(start).Milliseconds(),
	}
}

// pickAction 选与问题最相关的 action
func (c *ConnectorEntity) pickAction(q string) (int, float64) {
	qTokens := make(map[string]struct{})
	for _, t := range tokenize(q) {
		qTokens[t] = struct{}{}
	}
	bestIdx := 0
	bestScore := 0.0
	for i, a := range c.spec.Actions {
		// 合并 action 的名称、label、description 做匹配
		haystack := tokenize(a.Action + " " + a.Label + " " + a.Description)
		hit := 0
		seen := make(map[string]struct{})
		for _, t := range haystack {
			if _, ok := qTokens[t]; ok {
				if _, dup := seen[t]; !dup {
					hit++
					seen[t] = struct{}{}
				}
			}
		}
		// action 名称在问题里出现？强信号
		exactBonus := 0.0
		qLower := strings.ToLower(q)
		if strings.Contains(qLower, strings.ToLower(a.Action)) {
			exactBonus = 0.3
		}
		if strings.Contains(qLower, strings.ToLower(a.Label)) {
			exactBonus += 0.2
		}
		score := 0.0
		if len(qTokens) > 0 {
			score = float64(hit) / float64(len(qTokens))
		}
		score += exactBonus
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return bestIdx, bestScore
}

// extractParams 从问题中抽取 action 的参数（启发式）。
// 策略：
//  1. 对每个参数，从问题文本中"找它的取值"
//  2. 字符串型参数：找引号或冒号后的内容
//  3. 数字型参数：找附近的数字
//  4. 无法识别时：留空（调用会失败，但有 Error 返回供 LLM 汇总）
func (c *ConnectorEntity) extractParams(action *connector.ActionSpec, question string) map[string]any {
	params := make(map[string]any, len(action.Params))
	q := question
	for _, p := range action.Params {
		name := p.Name
		label := p.Label
		if label == "" {
			label = name
		}

		// 策略 A：label/name 在问题中出现了 "label=xxx"、"label：xxx" 格式
		for _, pattern := range []string{name + "=", name + " = ", label + "=", label + "：", label + "是", name + "是"} {
			idx := strings.Index(strings.ToLower(q), strings.ToLower(pattern))
			if idx >= 0 {
				remainder := q[idx+len(pattern):]
				// 取到下一个空白/标点
				end := len(remainder)
				for i, r := range remainder {
					if r == ' ' || r == '、' || r == ',' || r == '。' || r == '\n' || r == '"' || r == '\'' {
						end = i
						break
					}
				}
				val := strings.TrimSpace(remainder[:end])
				if val != "" {
					params[name] = val
				}
				break
			}
		}

		// 策略 B：数字参数 —— 提取问题中的第一个数字作为默认
		if _, ok := params[name]; !ok {
			if strings.Contains(strings.ToLower(p.Type), "number") ||
				strings.Contains(strings.ToLower(p.Type), "int") ||
				strings.Contains(strings.ToLower(p.Type), "float") ||
				strings.Contains(strings.ToLower(label), "数量") ||
				strings.Contains(strings.ToLower(label), "价格") ||
				strings.Contains(strings.ToLower(label), "数") {
				if num := extractFirstNumber(q); num != "" {
					params[name] = num
				}
			}
		}

		// 策略 C：有默认值直接用
		if _, ok := params[name]; !ok && p.Default != nil {
			params[name] = p.Default
		}
	}
	return params
}

// ---------- 格式化工具 ----------

// formatAsHumanReadable 把一段 JSON 响应格式化成人能读懂的简要文本
func formatAsHumanReadable(raw, actionName string) string {
	// 尝试 JSON 解析
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		// 成功解析：做结构化摘要
		return summarizeJSON(parsed, actionName, 0)
	}
	// 不是 JSON：直接截断返回
	if len(raw) > 400 {
		return raw[:400] + "..."
	}
	return raw
}

func summarizeJSON(v any, actionName string, depth int) string {
	if depth > 3 {
		return "..."
	}
	switch val := v.(type) {
	case map[string]any:
		if len(val) == 0 {
			return "(空)"
		}
		var sb strings.Builder
		sb.WriteString(actionName + " 返回: \n")
		for k, vv := range val {
			// 跳过常见的 status/code 等不具信息量的字段
			if k == "status" || k == "code" && depth == 0 {
				continue
			}
			switch inner := vv.(type) {
			case map[string]any, []any:
				sb.WriteString("  - ")
				sb.WriteString(k)
				sb.WriteString(": ")
				sb.WriteString(summarizeJSON(inner, "", depth+1))
				sb.WriteString("\n")
			default:
				s := fmt.Sprintf("%v", inner)
				if len(s) > 120 {
					s = s[:120] + "..."
				}
				sb.WriteString("  - ")
				sb.WriteString(k)
				sb.WriteString(": ")
				sb.WriteString(s)
				sb.WriteString("\n")
			}
		}
		return sb.String()
	case []any:
		if len(val) == 0 {
			return "(空列表)"
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("共 %d 项:\n", len(val)))
		limit := 5
		if len(val) < limit {
			limit = len(val)
		}
		for i := 0; i < limit; i++ {
			sb.WriteString("    [")
			sb.WriteString(iToStr(i))
			sb.WriteString("] ")
			s := fmt.Sprintf("%v", val[i])
			if len(s) > 100 {
				s = s[:100] + "..."
			}
			sb.WriteString(s)
			sb.WriteString("\n")
		}
		if len(val) > limit {
			sb.WriteString("    ... (还有 ")
			sb.WriteString(iToStr(len(val) - limit))
			sb.WriteString(" 项)\n")
		}
		return sb.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ---------- 小工具 ----------

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func uniq(xs []string) []string {
	seen := make(map[string]struct{}, len(xs))
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if _, ok := seen[x]; !ok {
			seen[x] = struct{}{}
			out = append(out, x)
		}
	}
	return out
}

// extractFirstNumber 从字符串中提取第一个数字（整数或小数）
func extractFirstNumber(s string) string {
	// 简化：扫描字符，找第一个 [0-9.]+ 的连续段
	inNum := false
	start := -1
	for i, r := range s {
		isDigit := r >= '0' && r <= '9'
		isDot := r == '.'
		if isDigit || (isDot && inNum) {
			if !inNum {
				inNum = true
				start = i
			}
		} else if inNum {
			return s[start:i]
		}
	}
	if inNum {
		return s[start:]
	}
	return ""
}

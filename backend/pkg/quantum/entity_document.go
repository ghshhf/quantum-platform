package quantum

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// DocumentEntity 把一份文档/文件变成一个可对话的"量子节点"。
//
// 这是"量子平台"的第一个 Entity 类型，体现了"无视格式差异"的核心思想：
//  - 输入只是一段文本（来自 PDF/TXT/Markdown/HTML/代码文件… 都一样）
//  - 对外暴露 Profile + Match + Execute，和任何其他 Entity 看起来没区别
//  - Bridge 调用它时，不需要知道它是文档还是 API 还是终端
//
// 检索算法（轻量 RAG v1）：
//   1. 初始化时按 chunkSize 把文档切成重叠的 chunks
//   2. Execute 时，把问题分词，统计每个 chunk 与问题的词共现率
//   3. 返回 TopK 个最相关 chunk 作为 EntityFragment
//
// （后续可以很容易替换成 embedding + 向量库检索，接口不变）
type DocumentEntity struct {
	name        string
	label       string
	description string
	chunks      []documentChunk
	chunkSize   int
	overlap     int
	keywords    []string
}

type documentChunk struct {
	index   int
	content string
	tokens  []string // 分词后的词袋（小写、去标点）
}

// NewDocumentEntity 从文本内容构造一个 Document Entity。
//
// name: 唯一标识；label: 展示名；description: 给 Bridge 的自我介绍
// content: 文档的纯文本内容；chunkSize: 每个 chunk 的字符数；overlap: 重叠字符数
func NewDocumentEntity(name, label, description, content string, chunkSize, overlap int) *DocumentEntity {
	if chunkSize <= 0 {
		chunkSize = 400
	}
	if overlap < 0 || overlap >= chunkSize {
		overlap = chunkSize / 4
	}
	de := &DocumentEntity{
		name:        name,
		label:       label,
		description: description,
		chunkSize:   chunkSize,
		overlap:     overlap,
	}
	de.chunks = splitIntoChunks(content, chunkSize, overlap)
	de.keywords = extractKeywords(de.chunks)
	return de
}

func (d *DocumentEntity) Profile() EntityProfile {
	desc := d.description
	if desc == "" {
		desc = "一份文档，包含以下关键词覆盖的内容: " + strings.Join(head(d.keywords, 15), ", ")
	}
	return EntityProfile{
		Name:        d.name,
		Label:       d.label,
		Kind:        KindDocument,
		Description: desc,
		Keywords:    head(d.keywords, 30),
		Metadata: map[string]string{
			"chunks":   iToStr(len(d.chunks)),
			"chunk_size": iToStr(d.chunkSize),
		},
		CreatedAt: time.Now(),
	}
}

// Match 用问题和文档关键词的交集比例打分（0~1）
func (d *DocumentEntity) Match(question string) float64 {
	qTokens := tokenize(question)
	if len(qTokens) == 0 {
		return 0
	}
	// 问题中词出现在文档关键词集中的比例
	hit := 0
	keywordSet := make(map[string]struct{}, len(d.keywords))
	for _, k := range d.keywords {
		keywordSet[k] = struct{}{}
	}
	seen := make(map[string]struct{})
	for _, t := range qTokens {
		if _, ok := keywordSet[t]; ok {
			if _, dup := seen[t]; !dup {
				hit++
				seen[t] = struct{}{}
			}
		}
	}
	// 稍微平滑一下：有 hit 就给个基础分
	raw := float64(hit) / float64(len(qTokens))
	if hit > 0 {
		raw = 0.15 + raw*0.85 // 命中任一关键词就至少 0.15 分
	}
	return clamp01(raw)
}

// Execute 从文档中找出与问题最相关的 chunks 返回。
// 同时支持 "summarize" 意图：返回文档的前后 chunk 作为"摘要"。
func (d *DocumentEntity) Execute(ctx context.Context, query EntityQuery) EntityResult {
	start := time.Now()
	profile := d.Profile()

	if len(d.chunks) == 0 {
		return EntityResult{
			Profile:   profile,
			Fragments: nil,
			LatencyMs: time.Since(start).Milliseconds(),
			Error:     "文档为空",
		}
	}

	topK := query.TopK
	if topK <= 0 {
		topK = 3
	}

	// summarize 意图：返回文档开头 + 中间 + 结尾 三个 chunk
	if query.Intent == "summarize" {
		frags := make([]EntityFragment, 0, 3)
		indices := []int{0, len(d.chunks) / 2, len(d.chunks) - 1}
		for _, idx := range indices {
			frags = append(frags, EntityFragment{
				EntityName: d.name,
				SourceRef:  "chunk " + iToStr(idx) + "/" + iToStr(len(d.chunks)),
				Content:    d.chunks[idx].content,
				Confidence: 0.9,
			})
		}
		return EntityResult{
			Profile:   profile,
			Fragments: frags,
			LatencyMs: time.Since(start).Milliseconds(),
		}
	}

	// 一般检索意图：对每个 chunk 计算与问题的相关度
	qTokens := tokenize(query.Question)
	if len(qTokens) == 0 {
		// 问题没有有效词 → 返回开头 chunks 作为兜底
		frags := make([]EntityFragment, 0, topK)
		for i := 0; i < topK && i < len(d.chunks); i++ {
			frags = append(frags, EntityFragment{
				EntityName: d.name,
				SourceRef:  "chunk " + iToStr(i) + "/" + iToStr(len(d.chunks)),
				Content:    d.chunks[i].content,
				Confidence: 0.3,
			})
		}
		return EntityResult{
			Profile:   profile,
			Fragments: frags,
			LatencyMs: time.Since(start).Milliseconds(),
		}
	}

	type scoredChunk struct {
		idx   int
		score float64
	}
	scored := make([]scoredChunk, len(d.chunks))
	qTokenSet := make(map[string]struct{}, len(qTokens))
	for _, t := range qTokens {
		qTokenSet[t] = struct{}{}
	}
	// 额外加 2-gram 匹配（对中文特别有用）
	qBigrams := makeBigrams(qTokens)

	for i, chunk := range d.chunks {
		hit := 0
		seen := make(map[string]struct{})
		for _, t := range chunk.tokens {
			if _, ok := qTokenSet[t]; ok {
				if _, dup := seen[t]; !dup {
					hit++
					seen[t] = struct{}{}
				}
			}
		}
		// bigram 命中加分
		bigramHit := 0
		if len(chunk.tokens) >= 2 {
			chunkBigrams := makeBigrams(chunk.tokens)
			bgSeen := make(map[string]struct{})
			for bg := range chunkBigrams {
				if _, ok := qBigrams[bg]; ok {
					if _, dup := bgSeen[bg]; !dup {
						bigramHit++
						bgSeen[bg] = struct{}{}
					}
				}
			}
		}

		// 打分：词命中 60% + bigram 命中 40%，长度做平滑
		score := 0.0
		if len(qTokens) > 0 {
			score += 0.6 * float64(hit) / float64(len(qTokens))
		}
		if len(qBigrams) > 0 {
			score += 0.4 * float64(bigramHit) / float64(len(qBigrams))
		}
		scored[i] = scoredChunk{idx: i, score: score}
	}

	// 按分数降序取 topK
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	frags := make([]EntityFragment, 0, topK)
	for i := 0; i < topK && i < len(scored) && scored[i].score > 0; i++ {
		frags = append(frags, EntityFragment{
			EntityName: d.name,
			SourceRef:  "chunk " + iToStr(scored[i].idx) + "/" + iToStr(len(d.chunks)),
			Content:    d.chunks[scored[i].idx].content,
			Confidence: clamp01(scored[i].score),
		})
	}

	if len(frags) == 0 {
		// 没找到强相关的，返回开头 chunks 作为"我没找到明确答案，但给你看最前面的内容"
		for i := 0; i < topK && i < len(d.chunks); i++ {
			frags = append(frags, EntityFragment{
				EntityName: d.name,
				SourceRef:  "chunk " + iToStr(i) + "/" + iToStr(len(d.chunks)),
				Content:    d.chunks[i].content,
				Confidence: 0.2,
			})
		}
	}

	return EntityResult{
		Profile:   profile,
		Fragments: frags,
		LatencyMs: time.Since(start).Milliseconds(),
	}
}

// ---------- 文本处理工具 ----------

var (
	// 中英文标点/空白符
	nonWordRe = regexp.MustCompile(`[\s\p{P}\p{S}]+`)
	// 常见停用词（中英文混合，极简版）
	stopwords = map[string]struct{}{
		"the": {}, "a": {}, "and": {}, "or": {}, "of": {}, "to": {}, "in": {},
		"on": {}, "for": {}, "with": {}, "is": {}, "are": {}, "was": {}, "were": {},
		"this": {}, "that": {}, "it": {}, "its": {}, "be": {}, "by": {}, "an": {},
		"":    {}, "的": {}, "了": {}, "和": {}, "与": {}, "及": {}, "是": {},
		"在": {}, "有": {}, "也": {}, "但": {}, "不": {}, "我": {}, "你": {},
		"他": {}, "她": {}, "它": {}, "这": {}, "那": {}, "就": {}, "都": {},
		"可以": {}, "能": {}, "会": {}, "一个": {}, "一些": {}, "什么": {},
		"哪个": {}, "哪些": {}, "多少": {}, "怎么": {}, "如何": {}, "为什么": {},
	}
)

// tokenize 把字符串切成词袋：英文按词/空白切，中文按字切（字 + 相邻 bigram 就够用）。
// 这是"不需要分词库"的中文最小可用方案。
func tokenize(s string) []string {
	s = strings.ToLower(s)
	// 按非文字字符切（包括中英文标点、空白、符号）
	parts := nonWordRe.Split(s, -1)

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		// 判断是否纯 ASCII（英文/数字）
		if isASCIILetters(p) {
			if _, skip := stopwords[p]; !skip {
				out = append(out, p)
			}
			continue
		}
		// 中文/混合：按 rune 拆成单字，停用词过滤
		for _, r := range p {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				continue
			}
			ch := string(r)
			if _, skip := stopwords[ch]; skip {
				continue
			}
			out = append(out, ch)
		}
	}
	return out
}

func isASCIILetters(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func makeBigrams(tokens []string) map[string]struct{} {
	out := make(map[string]struct{}, len(tokens))
	for i := 0; i < len(tokens)-1; i++ {
		out[tokens[i]+"|"+tokens[i+1]] = struct{}{}
	}
	return out
}

// splitIntoChunks 把文档按 chunkSize 字符切分，相邻 chunks 有 overlap 重叠。
func splitIntoChunks(content string, chunkSize, overlap int) []documentChunk {
	if content == "" {
		return nil
	}
	runes := []rune(content)
	chunks := make([]documentChunk, 0, len(runes)/chunkSize+1)
	step := chunkSize - overlap
	if step <= 0 {
		step = 1
	}
	idx := 0
	for start := 0; start < len(runes); start += step {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunkStr := string(runes[start:end])
		chunks = append(chunks, documentChunk{
			index:   idx,
			content: chunkStr,
			tokens:  tokenize(chunkStr),
		})
		idx++
		// 最后一个 chunk 已经触到文档末尾，停止
		if end == len(runes) {
			break
		}
	}
	return chunks
}

// extractKeywords 从 chunks 中提取"高频但不是停用词"的关键词，作为 Profile 的关键词。
func extractKeywords(chunks []documentChunk) []string {
	freq := make(map[string]int)
	total := 0
	for _, c := range chunks {
		for _, t := range c.tokens {
			freq[t]++
			total++
		}
	}
	// 取频率前 30 的词
	type pair struct {
		t string
		n int
	}
	list := make([]pair, 0, len(freq))
	for t, n := range freq {
		list = append(list, pair{t, n})
	}
	// 简单排序
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].n > list[i].n {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	out := make([]string, 0, 30)
	for i := 0; i < len(list) && i < 30; i++ {
		// 过滤太短/太常见的词
		if len(list[i].t) >= 1 && list[i].n >= 2 {
			out = append(out, list[i].t)
		}
	}
	return out
}

// ---------- 小型工具 ----------

func head(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return xs[:n]
}

func iToStr(n int) string {
	// 超轻量 int->string，无需走标准库
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

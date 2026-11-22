package service

import (
	"strings"
	"unicode"
)

// 3D 结构参考视频（白模序列）在 H3 提示词里的固定标签。
// 依据：ComfyUI 侧 C_AD.py 会为每条 ref_video 分配 "<Video N>" 标签，且只认提示词里
// 显式出现的标签；不写这句，参考视频就只是挂在那里，文字上没有任何约束。
const h3Video1Tag = "<Video 1>"

// 三句措辞已获用户批准（中文/英文两版都由本文件确定性写入，不交给模型自由发挥）。
func h3Video1DefinitionLine(zh bool) string {
	if zh {
		return "<Video 1> 是 3D 编辑器导出的机位结构参考（白模，无材质）：只提供镜头路径、机位高度、推拉摇移/环绕的运动节奏，以及主体在画幅内的走位比例；不提供角色外观、材质、光照与配色，画面内容一律以文字与 <Picture N> 为准。"
	}
	return "<Video 1> is the camera-structure reference exported from the 3D editor (untextured clay): it only defines the camera path, camera height, the pacing of push/pull/pan/tilt/orbit moves, and the subject's on-screen proportion; it does not define character appearance, materials, lighting or palette - on-screen content follows the text and <Picture N>."
}

func h3Video1RetentionLine(zh bool) string {
	if zh {
		return "<Video 1> (出现在 [Shot 1]): structure_only - 只复刻镜头轨迹、机位与走位节奏，忽略其白模材质、配色与光照。"
	}
	return "<Video 1> (appears in [Shot 1]): structure_only - replicate only the camera trajectory, camera position and blocking rhythm; ignore its clay material, palette and lighting."
}

func h3Video1DirectionSentence(zh bool) string {
	if zh {
		return "[Shot 1] 镜头严格复刻 <Video 1> 的机位路径与运动节奏，起幅构图对齐其首帧；主体动作与外观按文字与 <Picture N> 执行。"
	}
	return "[Shot 1] The camera replicates <Video 1>'s camera path and movement pacing exactly, matching its first frame for the opening framing; subject action and appearance follow the text and <Picture N>."
}

// injectH3Video1StructureReference 把这条标签写进 H3 六段协议的三处：
// subject_definitions（它是什么）、retention_analysis（只保留什么）、
// detailed_description（怎么用）。幂等：提示词里已有 <Video 1> 就原样返回。
// 只应在模板走 minimax_h3_ref2v_3d 时调用。
func injectH3Video1StructureReference(prompt string) string {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" || strings.Contains(trimmed, h3Video1Tag) {
		return prompt
	}
	if !strings.Contains(trimmed, "subject_definitions:") ||
		!strings.Contains(trimmed, "retention_analysis:") ||
		!strings.Contains(trimmed, "detailed_description:") {
		// 不是六段协议（例如用户手写的自由文本），不强行改造。
		return prompt
	}
	zh := h3PromptLooksChinese(trimmed)
	body := h3PromptSection(trimmed, "subject_definitions:")
	prompt = replaceH3PromptSection(trimmed, "subject_definitions:", joinNonEmpty(body, h3Video1DefinitionLine(zh)))
	body = h3PromptSection(prompt, "retention_analysis:")
	prompt = replaceH3PromptSection(prompt, "retention_analysis:", joinNonEmpty(body, h3Video1RetentionLine(zh)))
	body = h3PromptSection(prompt, "detailed_description:")
	return replaceH3PromptSection(prompt, "detailed_description:", insertH3Shot1Sentence(body, h3Video1DirectionSentence(zh)))
}

func joinNonEmpty(body, line string) string {
	if strings.TrimSpace(body) == "" {
		return line
	}
	return strings.TrimSpace(body) + "\n" + line
}

// insertH3Shot1Sentence 把机位指令放在 [Shot 1] 之前（风格前置说明之后），
// 没有 [Shot 1] 标记时退化为追加到段尾。
func insertH3Shot1Sentence(body, sentence string) string {
	if strings.Contains(body, sentence) {
		return body
	}
	body = strings.TrimSpace(body)
	if i := strings.Index(body, "[Shot 1]"); i >= 0 {
		head := strings.TrimRight(body[:i], "\n")
		tail := strings.TrimLeft(body[i:], "\n")
		if head == "" {
			return sentence + "\n" + tail
		}
		return head + "\n" + sentence + "\n" + tail
	}
	if body == "" {
		return sentence
	}
	return body + "\n" + sentence
}

// h3PromptLooksChinese 用汉字密度判断主稿语言：英文执行稿里会夹少量中文人名
// （如 <Subject 1> is 「阿青」），所以不能只看「有没有汉字」。
func h3PromptLooksChinese(prompt string) bool {
	han, total := 0, 0
	for _, r := range prompt {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if unicode.Is(unicode.Han, r) {
			han++
		}
	}
	if total == 0 {
		return false
	}
	return float64(han)/float64(total) > 0.1
}

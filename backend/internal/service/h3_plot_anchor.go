package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// h3PlotAnchorPattern also accepts ASCII and 【】 brackets, which models sometimes substitute.
var h3PlotAnchorPattern = regexp.MustCompile(`\s*[〔\[【]P([0-9]{1,3})[〕\]】]`)

var h3PlotNodeTextPattern = regexp.MustCompile(`[\p{Han}A-Za-z]`)

// h3PlotNodes splits combat Scene prose into ordered plot nodes for anchor checks. Quoted
// speech is removed (the voice ledger carries it); the speaking beat itself stays a node.
func h3PlotNodes(source string) []string {
	var nodes []string
	for _, entry := range h3SourceEventLedger(source) {
		visual := quotedDialoguePattern.ReplaceAllString(entry, "")
		visual = strings.TrimSpace(h3ShotMarkerPattern.ReplaceAllString(visual, ""))
		if h3PlotNodeTextPattern.MatchString(visual) {
			nodes = append(nodes, visual)
		}
	}
	return nodes
}

func h3PlotAnchorLabel(n int) string {
	return fmt.Sprintf("P%02d", n)
}

// formatH3PlotLedger renders the plot-node ledger and, when present, the voice ledger
// appended to combat action and repair requests.
func formatH3PlotLedger(nodes, voices []string) string {
	if len(nodes) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\n\n〔剧情节点账本〕在完成每个节点的那句话末尾紧跟一次〔P01〕形式的锚点；每个锚点恰好一次、按编号顺序，最后一个节点落在最后一个Shot里。锚点由系统删除。")
	for i, node := range nodes {
		out.WriteString("\n" + h3PlotAnchorLabel(i+1) + " " + node)
	}
	if len(voices) > 0 {
		out.WriteString("\n\n〔人声账本〕逐字、同序，只用于对齐口型时机；正文不得复述、改写或新增台词。")
		for _, voice := range voices {
			out.WriteString("\n" + voice)
		}
	}
	return out.String()
}

// checkH3PlotAnchors validates that every node anchor appears exactly once, in order, with the
// last node inside the last Shot, then returns the text with all anchors removed.
func checkH3PlotAnchors(text string, nodeCount int) (string, []string) {
	matches := h3PlotAnchorPattern.FindAllStringSubmatchIndex(text, -1)
	stripped := strings.TrimSpace(h3PlotAnchorPattern.ReplaceAllString(text, ""))
	if nodeCount <= 0 {
		return stripped, nil
	}
	var issues []string
	counts := map[int]int{}
	lastPosition := -1
	ordered := true
	previous := 0
	for _, match := range matches {
		n, _ := strconv.Atoi(text[match[2]:match[3]])
		if n < 1 || n > nodeCount {
			issues = append(issues, "出现账本外的剧情锚点"+h3PlotAnchorLabel(n))
			continue
		}
		counts[n]++
		if counts[n] > 1 {
			continue
		}
		if n < previous {
			ordered = false
		}
		previous = n
		if n == nodeCount {
			lastPosition = match[0]
		}
	}
	var missing, repeated []string
	for n := 1; n <= nodeCount; n++ {
		switch {
		case counts[n] == 0:
			missing = append(missing, h3PlotAnchorLabel(n))
		case counts[n] > 1:
			repeated = append(repeated, h3PlotAnchorLabel(n))
		}
	}
	if len(missing) > 0 {
		issues = append(issues, "缺少剧情锚点"+strings.Join(missing, "、")+"：对应剧情节点未完成或未标注")
	}
	if len(repeated) > 0 {
		issues = append(issues, "剧情锚点"+strings.Join(repeated, "、")+"重复：每个节点只能完成一次")
	}
	if !ordered {
		issues = append(issues, "剧情锚点顺序错乱：剧情节点必须按原文顺序完成")
	}
	if lastPosition >= 0 {
		if shots := h3ShotMarkerPattern.FindAllStringIndex(text, -1); len(shots) > 0 && lastPosition < shots[len(shots)-1][0] {
			issues = append(issues, "最后一个剧情节点没有落在最后一个Shot")
		}
	}
	return stripped, issues
}

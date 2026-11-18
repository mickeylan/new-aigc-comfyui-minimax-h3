package service

import (
	"encoding/json"
	"fmt"
	"strings"
)

// designSelection resolves one exact occurrence before any paid model call.
func designSelection(current, selection string, offset *int) (int, int, error) {
	text, selected := []rune(current), []rune(selection)
	if len(selected) == 0 {
		return 0, 0, fmt.Errorf("请重新选择要修改的文字")
	}
	start := 0
	if offset != nil {
		start = *offset
	} else {
		if strings.Count(current, selection) != 1 {
			return 0, 0, fmt.Errorf("选中文字不存在或出现多次，请重新选择具体位置")
		}
		start = len([]rune(current[:strings.Index(current, selection)]))
	}
	if start < 0 || start > len(text) || len(selected) > len(text)-start || string(text[start:start+len(selected)]) != selection {
		return 0, 0, fmt.Errorf("选中文字已变化，请重新选择")
	}
	return start, start + len(selected), nil
}

func (s *ProjectService) reviseAssetSelection(context, kind string, req AssetDesignRequest) (*AssetDesignDraft, error) {
	start, end, err := designSelection(req.Current, req.Selection, req.SelectionStart)
	if err != nil {
		return nil, err
	}
	system := "你是影视美术设定师。只改用户选中的片段，其他内容由程序原样保留。不要输出完整设定。" + assetDesignRules(kind) +
		`只输出 JSON：{"replacement":"选中片段的替换文字","changes":["修改后的完整设定相对基础资产的完整变化清单"]}。replacement 不能为空；没有基础资产时 changes 为空。`
	user := context + "当前稿：" + req.Current + "\n用户选中的片段：" + req.Selection + "\n这次的修改方向：" + req.Instruction
	output, err := s.textProvider.Chat(system, user)
	if err != nil {
		return nil, fmt.Errorf("AI 局部修改失败: %w", err)
	}
	var patch struct {
		Replacement *string  `json:"replacement"`
		Changes     []string `json:"changes"`
	}
	if json.Unmarshal([]byte(extractJSON(stripThinkBlocks(output))), &patch) != nil || patch.Replacement == nil || strings.TrimSpace(*patch.Replacement) == "" {
		return nil, fmt.Errorf("AI 未返回有效局部修改，原稿未改变；请重试或手工修改")
	}
	runes := []rune(req.Current)
	replacement := *patch.Replacement
	return &AssetDesignDraft{
		Description: string(runes[:start]) + replacement + string(runes[end:]),
		Sources:     []AssetDesignSource{{Tag: "按你的要求", Text: replacement}},
		Changes:     normalizeAssetDesignDirections(patch.Changes),
	}, nil
}

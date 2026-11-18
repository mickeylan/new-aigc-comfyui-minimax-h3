package service

import (
	"comfyui-console/internal/models"
	"fmt"
)

// Versioned bindings describe the reviewed bytes, not a mutable main-image pointer.
// Legacy selections deliberately remain readable until explicitly saved again.
func (s *ProjectService) bindSceneReference(pid uint, r SceneReferenceSelection, c SceneReferenceCandidate) SceneReferenceSelection {
	if s.upload == nil || s.upload.remote == nil {
		return r
	}
	r.BindingVersion = 1
	file := candidateFileMeta(pid, c)
	if c.CoveredImage != "" {
		file = FileMeta{TaskID: fmt.Sprint(pid), Name: c.CoveredImage}
	}
	r.BoundFile = file.TaskID + "/" + file.Name
	r.BoundSHA256 = s.fileSHA256(file)
	return r
}

func (s *ProjectService) referenceBindingIssue(pid uint, r SceneReferenceSelection, c SceneReferenceCandidate, exists bool) string {
	if !exists {
		return "来源已删除、未审核或不再属于当前项目"
	}
	if r.BindingVersion == 0 {
		return ""
	}
	current := s.bindSceneReference(pid, r, c)
	if r.BoundFile != current.BoundFile {
		return "来源图片版本已更换，请明确重新选择后保存"
	}
	if current.BoundSHA256 == "" {
		return "绑定文件缺失或无法读取"
	}
	if current.BoundSHA256 != r.BoundSHA256 {
		return "绑定文件内容已变化，请明确重新选择后保存"
	}
	return ""
}

func (s *ProjectService) validateSceneReferenceBindings(sc *models.Scene) error {
	selected, explicit := parseSceneReferences(sc)
	if !explicit {
		return nil
	}
	byKey := map[string]SceneReferenceCandidate{}
	for _, c := range s.sceneReferenceCandidates(sc) {
		byKey[c.Key] = c
	}
	for _, r := range selected {
		if r.BindingVersion == 0 || (!r.UseKrea2 && !r.UseH3) {
			continue
		}
		c, ok := byKey[referenceKey(r)]
		if issue := s.referenceBindingIssue(sc.ProjectID, r, c, ok); issue != "" {
			return fmt.Errorf("参考图%s失效：%s", referenceKey(r), issue)
		}
	}
	return nil
}

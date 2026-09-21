package service

import (
	"context"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

type promptProgramStub struct {
	response, system, user string
	err                    error
}

func (s *promptProgramStub) Name() string                      { return "stub" }
func (s *promptProgramStub) HealthCheck(context.Context) error { return nil }
func (s *promptProgramStub) Chat(system, user string) (string, error) {
	s.system, s.user = system, user
	return s.response, s.err
}

func TestQwenImage21T2IPromptProgram(t *testing.T) {
	p := &promptProgramStub{response: `{"rewritten_prompt":"A wide cinematic photograph of a colossal city above pale clouds. The lighting is warm from the left. The overall composition feels monumental and balanced.","wh_ratio":"16:9"}`}
	s := NewQwenImagePromptProgramService(p)
	got, err := s.Generate(QwenImage21T2IProgram, PromptProgramInput{Brief: "云海上的巨城", AspectRatio: "16:9"})
	if err != nil {
		t.Fatal(err)
	}
	if got.WHRatio != "16:9" || got.RatioFollow != "" || got.Provider != "stub" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if !strings.Contains(p.system, "finished image") || !strings.Contains(p.user, "云海上的巨城") {
		t.Fatal("official T2I contract not applied")
	}
}

func TestQwenImage21EditPromptBindsReferenceOrder(t *testing.T) {
	p := &promptProgramStub{response: `{"rewritten_prompt":"以<image2>作为画布，保持环境构图不变，将<image1>的人物身份放入场景并匹配光线。","wh_ratio":"","ratio_follow":"<image2>"}`}
	s := NewQwenImagePromptProgramService(p)
	got, err := s.Generate(QwenImage21EditProgram, PromptProgramInput{Brief: "人物进入场景", References: []PromptReference{{Role: "人物身份", Description: "青年男性"}, {Role: "目标场景", Description: "云海宫殿"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.RatioFollow != "<image2>" || !strings.Contains(p.user, "<image1>: role=人物身份") || !strings.Contains(p.user, "<image2>: role=目标场景") {
		t.Fatalf("reference order lost: result=%+v user=%s", got, p.user)
	}
}

func TestQwenImage21PromptProgramUsesBuiltinSkillAndAudit(t *testing.T) {
	ps := newTestProjectService(t)
	if err := ps.db.AutoMigrate(&models.Skill{}, &models.ProjectSkillConfig{}, &models.SkillAuditLog{}); err != nil {
		t.Fatal(err)
	}
	skills := NewSkillService(ps.db)
	if err := skills.InitSystemSkills(); err != nil {
		t.Fatal(err)
	}
	provider := &promptProgramStub{response: `{"rewritten_prompt":"A wide cinematic scene of a cloud city under warm directional light. The overall composition is monumental and balanced.","wh_ratio":"16:9"}`}
	service := NewQwenImagePromptProgramService(provider, skills)
	if _, err := service.Generate(QwenImage21T2IProgram, PromptProgramInput{Brief: "云上巨城"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(provider.system, "finished image") {
		t.Fatalf("builtin skill system prompt missing: %s", provider.system)
	}
	var audit models.SkillAuditLog
	if err := ps.db.Where("skill_code = ?", QwenImage21T2IProgram).First(&audit).Error; err != nil {
		t.Fatal("skill invocation was not audited: ", err)
	}
}

func TestQwenImage21PromptProgramValidation(t *testing.T) {
	s := NewQwenImagePromptProgramService(&promptProgramStub{})
	if _, err := s.Generate(QwenImage21T2IProgram, PromptProgramInput{Brief: "x", References: []PromptReference{{Role: "wrong"}}}); err == nil {
		t.Fatal("T2I accepted references")
	}
	if _, err := s.Generate(QwenImage21EditProgram, PromptProgramInput{Brief: "x"}); err == nil {
		t.Fatal("edit accepted no references")
	}
	if _, err := s.Generate("missing", PromptProgramInput{Brief: "x"}); err != ErrPromptProgramNotFound {
		t.Fatalf("unexpected unknown error: %v", err)
	}
}

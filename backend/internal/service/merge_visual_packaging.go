package service

import "fmt"

// Built-in looks are original procedural RGB LUTs, not imported third-party
// grading files. Only these identifiers may become FFmpeg filter expressions.
type clipVisualPackaging struct {
	Look           string `json:"look,omitempty"`
	TitleAnimation string `json:"title_animation,omitempty"`
	TitleStyle     string `json:"title_style,omitempty"`
}

func validMergeTransition(value string) bool {
	switch value {
	case "", "cut", "fade", "wipeleft", "slideright", "wiperight", "wipeup", "wipedown", "slideleft", "slideup", "slidedown", "dissolve", "fadeblack", "fadewhite", "circleopen", "circleclose":
		return true
	}
	return false
}
func validateVisualPackaging(p *clipVisualPackaging) error {
	if p == nil {
		return nil
	}
	switch p.Look {
	case "", "none", "warm", "cool", "muted", "monochrome":
	default:
		return fmt.Errorf("未知画面风格")
	}
	switch p.TitleAnimation {
	case "", "none", "fade", "slide", "typewriter":
	default:
		return fmt.Errorf("未知标题动画")
	}
	switch p.TitleStyle {
	case "", "caption", "chapter", "nameplate":
	default:
		return fmt.Errorf("未知标题样式")
	}
	return nil
}
func clipLookFilter(p *clipVisualPackaging) string {
	if p == nil {
		return ""
	}
	switch p.Look {
	case "warm":
		return ",lutrgb=r='clip(val*1.04+3,0,255)':g='clip(val*1.01,0,255)':b='clip(val*.96,0,255)'"
	case "cool":
		return ",lutrgb=r='clip(val*.96,0,255)':g='clip(val*1.01,0,255)':b='clip(val*1.04+3,0,255)'"
	case "muted":
		return ",eq=saturation=0.72:contrast=0.96:brightness=0.015"
	case "monochrome":
		return ",hue=s=0"
	}
	return ""
}
func validateOverlayMask(mask string) error {
	switch mask {
	case "", "none", "ellipse", "rounded":
		return nil
	}
	return fmt.Errorf("未知叠加蒙版")
}

// Source alpha is multiplied rather than replaced, preserving transparent PNGs.
func overlayMaskFilter(mask string) string {
	switch mask {
	case "ellipse":
		return ",geq=r='r(X,Y)':g='g(X,Y)':b='b(X,Y)':a='alpha(X,Y)*lte(pow((X-W/2)/(W/2),2)+pow((Y-H/2)/(H/2),2),1)'"
	case "rounded":
		return ",geq=r='r(X,Y)':g='g(X,Y)':b='b(X,Y)':a='alpha(X,Y)*lte(pow(max(abs(X-W/2)-(W/2-min(W,H)*0.12),0),2)+pow(max(abs(Y-H/2)-(H/2-min(W,H)*0.12),0),2),pow(min(W,H)*0.12,2))'"
	}
	return ""
}

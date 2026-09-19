// Package screenplay provides dependency-free screenplay interchange parsing and rendering.
package screenplay

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const (
	FormatFountain = "fountain"
	FormatFDX      = "fdx"
	FormatText     = "txt"
)

type Element struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Scene struct {
	Heading  string    `json:"heading"`
	Elements []Element `json:"elements"`
}

type Preview struct {
	Format string  `json:"format"`
	Title  string  `json:"title,omitempty"`
	Scenes []Scene `json:"scenes"`
}

func (p Preview) ElementCount() int {
	n := 0
	for _, scene := range p.Scenes {
		n += len(scene.Elements) + 1
	}
	return n
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimSpace(strings.TrimPrefix(s, "\ufeff"))
}

var scenePrefix = regexp.MustCompile(`(?i)^(INT\.?|EXT\.?|EST\.?|INT\.?/EXT\.?|EXT\.?/INT\.?|I/E\.?)\s`)

func isSceneHeading(s string) bool {
	t := strings.TrimSpace(strings.TrimPrefix(s, "."))
	return strings.HasPrefix(s, ".") || scenePrefix.MatchString(t)
}

func isTransition(s string) bool {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, ">") {
		return true
	}
	return t == strings.ToUpper(t) && (strings.HasSuffix(t, " TO:") || strings.HasSuffix(t, "TO:") || strings.HasSuffix(t, "CUT:"))
}

func isCharacter(s string) bool {
	t := strings.TrimSpace(strings.TrimPrefix(s, "@"))
	if t == "" || len([]rune(t)) > 60 || strings.HasSuffix(t, ".") || strings.HasSuffix(t, ":") {
		return false
	}
	hasLetter := false
	for _, r := range t {
		if unicode.IsLetter(r) {
			hasLetter = true
			if unicode.IsLower(r) {
				return false
			}
		}
	}
	return hasLetter
}

func addScene(p *Preview, heading string) *Scene {
	p.Scenes = append(p.Scenes, Scene{Heading: strings.TrimSpace(strings.TrimPrefix(heading, ".")), Elements: []Element{}})
	return &p.Scenes[len(p.Scenes)-1]
}

func addElement(scene *Scene, typ, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if n := len(scene.Elements); n > 0 && scene.Elements[n-1].Type == typ && typ == "action" {
		scene.Elements[n-1].Text += "\n" + text
		return
	}
	scene.Elements = append(scene.Elements, Element{Type: typ, Text: text})
}

// ParseFountain parses the common, line-oriented Fountain screenplay elements.
func ParseFountain(input string) (Preview, error) {
	input = normalize(input)
	if input == "" {
		return Preview{}, fmt.Errorf("screenplay is empty")
	}
	p := Preview{Format: FormatFountain, Scenes: []Scene{}}
	lines := strings.Split(input, "\n")
	// Read the standard title-page Title field before screenplay content.
	for i, line := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "title:") {
			p.Title = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			lines[i] = ""
			continue
		}
		if strings.TrimSpace(line) != "" {
			break
		}
	}
	var current *Scene
	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			i++
			continue
		}
		if isSceneHeading(line) {
			current = addScene(&p, line)
			i++
			continue
		}
		if current == nil {
			current = addScene(&p, "SCENE 1")
		}
		if isTransition(line) {
			addElement(current, "transition", strings.Trim(line, ">< "))
			i++
			continue
		}
		next := ""
		if i+1 < len(lines) {
			next = strings.TrimSpace(lines[i+1])
		}
		if isCharacter(line) && next != "" && !isSceneHeading(next) && !isTransition(next) {
			addElement(current, "character", strings.TrimSpace(strings.TrimPrefix(line, "@")))
			i++
			if i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "(") && strings.HasSuffix(strings.TrimSpace(lines[i]), ")") {
				addElement(current, "parenthetical", strings.Trim(strings.TrimSpace(lines[i]), "()"))
				i++
			}
			var dialogue []string
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				if t == "" || isSceneHeading(t) || isTransition(t) {
					break
				}
				dialogue = append(dialogue, t)
				i++
			}
			addElement(current, "dialogue", strings.Join(dialogue, "\n"))
			continue
		}
		addElement(current, "action", strings.TrimPrefix(line, "!"))
		i++
	}
	if len(p.Scenes) == 0 {
		return Preview{}, fmt.Errorf("screenplay has no content")
	}
	return p, nil
}

type fdxDocument struct {
	XMLName xml.Name   `xml:"FinalDraft"`
	Content fdxContent `xml:"Content"`
}
type fdxContent struct {
	Paragraphs []fdxParagraph `xml:"Paragraph"`
}
type fdxParagraph struct {
	Type  string    `xml:"Type,attr"`
	Texts []fdxText `xml:"Text"`
}
type fdxText struct {
	Value string `xml:",chardata"`
}

func paragraphText(paragraph fdxParagraph) string {
	var b strings.Builder
	for _, text := range paragraph.Texts {
		b.WriteString(text.Value)
	}
	return strings.TrimSpace(b.String())
}

// ParseFDX parses the minimal Final Draft XML paragraph vocabulary used by the preview DTO.
func ParseFDX(input string) (Preview, error) {
	input = normalize(input)
	if input == "" {
		return Preview{}, fmt.Errorf("screenplay is empty")
	}
	var doc fdxDocument
	decoder := xml.NewDecoder(strings.NewReader(input))
	decoder.Strict = true
	if err := decoder.Decode(&doc); err != nil {
		return Preview{}, fmt.Errorf("invalid FDX: %w", err)
	}
	if doc.XMLName.Local != "FinalDraft" {
		return Preview{}, fmt.Errorf("invalid FDX root")
	}
	p := Preview{Format: FormatFDX, Scenes: []Scene{}}
	var current *Scene
	for _, paragraph := range doc.Content.Paragraphs {
		text := paragraphText(paragraph)
		if text == "" {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(paragraph.Type))
		if typ == "scene heading" {
			current = addScene(&p, text)
			continue
		}
		if current == nil {
			current = addScene(&p, "SCENE 1")
		}
		switch typ {
		case "action", "character", "dialogue", "parenthetical", "transition":
			addElement(current, typ, text)
		}
	}
	if len(p.Scenes) == 0 {
		return Preview{}, fmt.Errorf("FDX has no supported screenplay content")
	}
	return p, nil
}

func Parse(format, input string) (Preview, error) {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), ".")) {
	case FormatFountain, "fountain.txt":
		return ParseFountain(input)
	case FormatFDX:
		return ParseFDX(input)
	case FormatText, "text":
		p, err := ParseFountain(input)
		p.Format = FormatText
		return p, err
	default:
		return Preview{}, fmt.Errorf("unsupported screenplay format")
	}
}

func WriteFountain(p Preview) string {
	var b strings.Builder
	if strings.TrimSpace(p.Title) != "" {
		fmt.Fprintf(&b, "Title: %s\n\n", strings.TrimSpace(p.Title))
	}
	for si, scene := range p.Scenes {
		if si > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimSpace(scene.Heading))
		b.WriteString("\n\n")
		for _, e := range scene.Elements {
			t := strings.TrimSpace(e.Text)
			if t == "" {
				continue
			}
			switch e.Type {
			case "parenthetical":
				fmt.Fprintf(&b, "(%s)\n", strings.Trim(t, "()"))
			case "transition":
				fmt.Fprintf(&b, ">%s\n\n", strings.Trim(t, ">< "))
			case "character":
				fmt.Fprintf(&b, "%s\n", strings.ToUpper(t))
			case "dialogue":
				fmt.Fprintf(&b, "%s\n\n", t)
			default:
				fmt.Fprintf(&b, "%s\n\n", t)
			}
		}
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func WriteText(p Preview) string {
	var b strings.Builder
	for si, scene := range p.Scenes {
		if si > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s\n", strings.TrimSpace(scene.Heading))
		for _, e := range scene.Elements {
			t := strings.TrimSpace(e.Text)
			if t == "" {
				continue
			}
			switch e.Type {
			case "character":
				fmt.Fprintf(&b, "%s: ", t)
			case "dialogue":
				fmt.Fprintf(&b, "%s\n", t)
			case "parenthetical":
				fmt.Fprintf(&b, "(%s) ", strings.Trim(t, "()"))
			default:
				fmt.Fprintf(&b, "%s\n", t)
			}
		}
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func WriteFDX(p Preview) (string, error) {
	doc := fdxDocument{Content: fdxContent{Paragraphs: []fdxParagraph{}}}
	for _, scene := range p.Scenes {
		doc.Content.Paragraphs = append(doc.Content.Paragraphs, fdxParagraph{Type: "Scene Heading", Texts: []fdxText{{Value: strings.TrimSpace(scene.Heading)}}})
		for _, e := range scene.Elements {
			types := map[string]string{"action": "Action", "character": "Character", "dialogue": "Dialogue", "parenthetical": "Parenthetical", "transition": "Transition"}
			if typ := types[e.Type]; typ != "" && strings.TrimSpace(e.Text) != "" {
				doc.Content.Paragraphs = append(doc.Content.Paragraphs, fdxParagraph{Type: typ, Texts: []fdxText{{Value: strings.TrimSpace(e.Text)}}})
			}
		}
	}
	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	out.WriteString(xml.Header)
	out.Write(body)
	out.WriteByte('\n')
	return out.String(), nil
}

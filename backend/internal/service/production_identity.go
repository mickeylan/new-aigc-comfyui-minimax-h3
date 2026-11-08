package service

import (
	"encoding/json"
	"strings"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

type productionIdentityRegistry struct {
	Characters []models.Character               `json:"characters"`
	Assets     []models.Asset                   `json:"assets"`
	Aliases    []models.CharacterAliasCandidate `json:"confirmed_aliases"`
}

func loadProductionIdentities(db *gorm.DB, projectID uint) (*productionIdentityRegistry, error) {
	r := &productionIdentityRegistry{}
	if err := db.Where("project_id=?", projectID).Order("id").Find(&r.Characters).Error; err != nil {
		return nil, err
	}
	if err := db.Where("project_id=?", projectID).Order("id").Find(&r.Assets).Error; err != nil {
		return nil, err
	}
	if db.Migrator().HasTable(&models.CharacterAliasCandidate{}) {
		if err := db.Where("project_id=? AND status=?", projectID, "confirmed").Order("id").Find(&r.Aliases).Error; err != nil {
			return nil, err
		}
	}
	return r, nil
}
func identityNameKey(s string) string { return strings.Join(strings.Fields(strings.TrimSpace(s)), " ") }
func (r *productionIdentityRegistry) character(name string) string {
	name = identityNameKey(name)
	for _, a := range r.Aliases {
		if identityNameKey(a.Alias) == name && strings.TrimSpace(a.CanonicalName) != "" {
			return strings.TrimSpace(a.CanonicalName)
		}
	}
	for _, c := range r.Characters {
		if identityNameKey(c.Name) == name {
			return c.Name
		}
	}
	// A full existing proper name inside a performance description is still that
	// identity. Require exactly one match; never fuzzy-match generic young man/girl.
	found := ""
	for _, c := range r.Characters {
		key := identityNameKey(c.Name)
		if len([]rune(key)) >= 3 && strings.Contains(name, key) {
			if found != "" && found != c.Name {
				return name
			}
			found = c.Name
		}
	}
	if found != "" {
		return found
	}
	return name
}
func (r *productionIdentityRegistry) asset(kind, name string) string {
	name = identityNameKey(name)
	for _, a := range r.Assets {
		if a.Kind == kind && identityNameKey(a.Name) == name {
			return a.Name
		}
	}
	if kind != AssetKindLocation {
		return name
	}
	// Only frame/state qualifiers are removable, not arbitrary place suffixes
	// (e.g. 荒地入口 and 荒地神殿 may be distinct locations).
	base := strings.TrimPrefix(name, "战后的")
	base = strings.TrimPrefix(base, "战后")
	for _, sep := range []string{"，", ","} {
		if i := strings.Index(base, sep); i >= 0 {
			base = strings.TrimSpace(base[:i])
			break
		}
	}
	var found string
	for _, a := range r.Assets {
		if a.Kind != kind {
			continue
		}
		key := identityNameKey(a.Name)
		if base == key || base == key+"上空" {
			if found != "" && found != a.Name {
				return name
			}
			found = a.Name
		}
	}
	if found != "" {
		return found
	}
	return name
}
func (r *productionIdentityRegistry) normalizeScenes(scenes []scriptScene) {
	normalize := func(names []string) []string {
		out := []string{}
		seen := map[string]bool{}
		for _, name := range names {
			v := r.character(name)
			if v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
		return out
	}
	for i := range scenes {
		sc := &scenes[i]
		sc.Characters = normalize(sc.Characters)
		sc.VisibleCharacters = normalize(sc.VisibleCharacters)
		sc.VoiceCharacters = normalize(sc.VoiceCharacters)
		sc.MentionedCharacters = normalize(sc.MentionedCharacters)
		sc.Location = r.asset(AssetKindLocation, sc.Location)
		for j := range sc.Props {
			sc.Props[j] = r.asset(AssetKindProp, sc.Props[j])
		}
	}
}
func (r *productionIdentityRegistry) promptCatalogue() string {
	chars, locations, props := []string{}, []string{}, []string{}
	aliases := map[string]string{}
	for _, c := range r.Characters {
		chars = append(chars, c.Name)
	}
	for _, a := range r.Assets {
		if a.Kind == AssetKindLocation {
			locations = append(locations, a.Name)
		} else if a.Kind == AssetKindProp {
			props = append(props, a.Name)
		}
	}
	for _, a := range r.Aliases {
		aliases[a.Alias] = a.CanonicalName
	}
	b, _ := json.Marshal(map[string]any{"characters": chars, "locations": locations, "props": props, "confirmed_aliases": aliases})
	return "项目已有资产身份（同一人物、地点或道具跨集必须复用这些名称；动作、方位、战前战后状态写入content，不得拼进资产名；不得把女童、青年等临时称呼另建人物。确有新身份才新增，不得猜测未确认别名）：" + string(b)
}

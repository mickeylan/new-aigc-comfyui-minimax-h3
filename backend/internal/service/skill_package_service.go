package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

var skillPackageCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,119}$`)

type SkillPackageManifest struct {
	Name        string   `json:"name"`
	Code        string   `json:"code"`
	Version     int      `json:"version"`
	Description string   `json:"description"`
	Stage       string   `json:"stage"`
	Operation   string   `json:"operation"`
	PromptFile  string   `json:"prompt_file"`
	SystemFile  string   `json:"system_file"`
	References  []string `json:"references"`
	Enabled     *bool    `json:"enabled,omitempty"`
	SortOrder   int      `json:"sort_order"`
}

type SkillPackageImportResult struct {
	Code        string `json:"code"`
	Version     int    `json:"version"`
	ContentHash string `json:"content_hash"`
	SkillID     uint   `json:"skill_id"`
	Created     bool   `json:"created"`
}

// ImportSkillPackages imports immutable prompt-only packages. It cannot register tools or execute code.
func (s *SkillService) ImportSkillPackages(root string) ([]SkillPackageImportResult, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("skill package directory is empty")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	results := make([]SkillPackageImportResult, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		result, err := s.importSkillPackage(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("import skill package %s: %w", entry.Name(), err)
		}
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Code == results[j].Code {
			return results[i].Version < results[j].Version
		}
		return results[i].Code < results[j].Code
	})
	return results, nil
}

func (s *SkillService) importSkillPackage(dir string) (SkillPackageImportResult, error) {
	manifestBytes, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return SkillPackageImportResult{}, err
	}
	var manifest SkillPackageManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return SkillPackageImportResult{}, fmt.Errorf("invalid manifest: %w", err)
	}
	if err := validateSkillPackageManifest(manifest); err != nil {
		return SkillPackageImportResult{}, err
	}
	prompt, err := readSkillPackageText(dir, manifest.PromptFile)
	if err != nil {
		return SkillPackageImportResult{}, fmt.Errorf("read prompt: %w", err)
	}
	system, err := readSkillPackageText(dir, manifest.SystemFile)
	if err != nil {
		return SkillPackageImportResult{}, fmt.Errorf("read system prompt: %w", err)
	}
	references := append([]string(nil), manifest.References...)
	sort.Strings(references)
	hash := sha256.New()
	hash.Write(manifestBytes)
	hash.Write([]byte{0})
	hash.Write([]byte(prompt))
	hash.Write([]byte{0})
	hash.Write([]byte(system))
	for _, name := range references {
		content, err := readSkillPackageText(dir, name)
		if err != nil {
			return SkillPackageImportResult{}, fmt.Errorf("read reference %s: %w", name, err)
		}
		hash.Write([]byte{0})
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write([]byte(content))
		system += "\n\n[Reference: " + name + "]\n" + content
	}
	contentHash := hex.EncodeToString(hash.Sum(nil))

	result := SkillPackageImportResult{Code: manifest.Code, Version: manifest.Version, ContentHash: contentHash}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var existing models.Skill
		err := tx.Where("code = ? AND version = ?", manifest.Code, manifest.Version).First(&existing).Error
		if err == nil {
			if existing.PromptTemplate != prompt || existing.SystemPrompt != system || existing.Stage != manifest.Stage || existing.Operation != manifest.Operation {
				return fmt.Errorf("skill %s version %d already exists with different content; publish a new version", manifest.Code, manifest.Version)
			}
			result.SkillID = existing.ID
			var install models.SkillPackageInstall
			if err := tx.Where("skill_id = ?", existing.ID).First(&install).Error; err == nil {
				if install.ContentHash != contentHash {
					return fmt.Errorf("skill package hash changed without a version bump")
				}
				return nil
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return tx.Create(&models.SkillPackageInstall{SkillID: existing.ID, Code: manifest.Code, Version: manifest.Version, SourcePath: dir, ContentHash: contentHash, ManifestJSON: string(manifestBytes), ImportedAt: time.Now()}).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		enabled := false
		if manifest.Enabled != nil {
			enabled = *manifest.Enabled
		}
		var parent models.Skill
		var parentID *uint
		if err := tx.Where("code = ?", manifest.Code).Order("version DESC, id DESC").First(&parent).Error; err == nil {
			parentID = &parent.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		skill := models.Skill{Name: manifest.Name, Code: manifest.Code, Version: manifest.Version, Description: manifest.Description, Stage: manifest.Stage, Operation: manifest.Operation, PromptTemplate: prompt, SystemPrompt: system, Enabled: enabled, SortOrder: manifest.SortOrder, ParentID: parentID}
		if err := tx.Create(&skill).Error; err != nil {
			return err
		}
		if !enabled {
			if err := tx.Model(&skill).Update("enabled", false).Error; err != nil {
				return err
			}
			skill.Enabled = false
		}
		result.SkillID, result.Created = skill.ID, true
		return tx.Create(&models.SkillPackageInstall{SkillID: skill.ID, Code: manifest.Code, Version: manifest.Version, SourcePath: dir, ContentHash: contentHash, ManifestJSON: string(manifestBytes), ImportedAt: time.Now()}).Error
	})
	return result, err
}

func validateSkillPackageManifest(manifest SkillPackageManifest) error {
	if strings.TrimSpace(manifest.Name) == "" || !skillPackageCodePattern.MatchString(manifest.Code) || manifest.Version <= 0 {
		return fmt.Errorf("name, valid code and positive version are required")
	}
	if !isValidStage(manifest.Stage) {
		return fmt.Errorf("invalid skill stage %q", manifest.Stage)
	}
	if strings.TrimSpace(manifest.Operation) == "" {
		return fmt.Errorf("operation is required")
	}
	if strings.TrimSpace(manifest.PromptFile) == "" || strings.TrimSpace(manifest.SystemFile) == "" {
		return fmt.Errorf("prompt_file and system_file are required")
	}
	return nil
}

func readSkillPackageText(dir, relative string) (string, error) {
	relative = filepath.Clean(strings.TrimSpace(relative))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path must stay inside package")
	}
	content, err := os.ReadFile(filepath.Join(dir, relative))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

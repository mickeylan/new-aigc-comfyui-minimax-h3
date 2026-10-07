package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const fontManifestFile = "manifest.json"

var approvedFontLicenses = map[string]bool{
	"OFL-1.1":    true,
	"Apache-2.0": true,
}

type FontRegistryEntry struct {
	Code              string `json:"code"`
	DisplayName       string `json:"display_name"`
	Family            string `json:"family"`
	File              string `json:"file"`
	SHA256            string `json:"sha256"`
	Version           string `json:"version"`
	LicenseID         string `json:"license_id"`
	LicenseFile       string `json:"license_file"`
	SourceURL         string `json:"source_url"`
	CommercialUse     bool   `json:"commercial_use"`
	Redistribution    bool   `json:"redistribution"`
	Embedding         bool   `json:"embedding"`
	SupportsChinese   bool   `json:"supports_chinese"`
	SupportsVertical  bool   `json:"supports_vertical"`
	Enabled           bool   `json:"enabled"`
	Verified          bool   `json:"verified"`
	VerificationError string `json:"verification_error,omitempty"`
}

type FontRegistry struct{ root string }

func NewFontRegistry(dataDir string) *FontRegistry {
	return &FontRegistry{root: filepath.Join(dataDir, "fonts")}
}

func safeRegistryFile(root, relative string) (string, error) {
	relative = strings.TrimSpace(relative)
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("字体清单路径无效")
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean != relative || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("字体清单路径无效")
	}
	path := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("字体文件越出白名单目录")
	}
	return path, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (r *FontRegistry) List() ([]FontRegistryEntry, error) {
	if r == nil || strings.TrimSpace(r.root) == "" {
		return []FontRegistryEntry{}, nil
	}
	data, err := os.ReadFile(filepath.Join(r.root, fontManifestFile))
	if os.IsNotExist(err) {
		return []FontRegistryEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []FontRegistryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("字体白名单清单无效: %w", err)
	}
	seen := map[string]bool{}
	for i := range entries {
		entry := &entries[i]
		entry.Code = strings.TrimSpace(entry.Code)
		entry.LicenseID = strings.TrimSpace(entry.LicenseID)
		entry.SHA256 = strings.ToLower(strings.TrimSpace(entry.SHA256))
		entry.Verified = false
		entry.VerificationError = ""
		if entry.Code == "" || seen[entry.Code] {
			entry.VerificationError = "字体 code 为空或重复"
			continue
		}
		seen[entry.Code] = true
		if !approvedFontLicenses[entry.LicenseID] || !entry.CommercialUse || !entry.Redistribution || !entry.Embedding {
			entry.VerificationError = "字体许可证未进入商用、再分发和嵌入白名单"
			continue
		}
		fontPath, pathErr := safeRegistryFile(r.root, entry.File)
		licensePath, licenseErr := safeRegistryFile(r.root, entry.LicenseFile)
		if pathErr != nil || licenseErr != nil {
			entry.VerificationError = "字体或许可证路径无效"
			continue
		}
		if _, err := os.Stat(licensePath); err != nil {
			entry.VerificationError = "许可证文件不存在"
			continue
		}
		actual, err := fileSHA256(fontPath)
		if err != nil {
			entry.VerificationError = "字体文件不存在或不可读"
			continue
		}
		if len(entry.SHA256) != 64 || actual != entry.SHA256 {
			entry.VerificationError = "字体文件 SHA-256 不匹配"
			continue
		}
		if !entry.SupportsChinese {
			entry.VerificationError = "字体未声明中文覆盖"
			continue
		}
		entry.Verified = entry.Enabled
	}
	return entries, nil
}

func (r *FontRegistry) FilePath(entry *FontRegistryEntry) (string, error) {
	if r == nil || entry == nil {
		return "", fmt.Errorf("字体登记不存在")
	}
	return safeRegistryFile(r.root, entry.File)
}

func (r *FontRegistry) Root() string {
	if r == nil {
		return ""
	}
	return r.root
}

func (r *FontRegistry) Approved(code string) (*FontRegistryEntry, error) {
	entries, err := r.List()
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].Code == code {
			if !entries[i].Verified {
				return nil, fmt.Errorf("字体 %s 未通过发行合规校验: %s", code, entries[i].VerificationError)
			}
			return &entries[i], nil
		}
	}
	return nil, fmt.Errorf("字体 %s 未登记", code)
}

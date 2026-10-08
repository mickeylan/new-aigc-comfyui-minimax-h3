package config

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig   `yaml:"server"`
	Comfy        ComfyConfig    `yaml:"comfy"`
	Storage      StorageConfig  `yaml:"storage"`
	GPU          GPUConfig      `yaml:"gpu"`
	Remote       RemoteConfig   `yaml:"remote"`
	IndexTTS     IndexTTSConfig `yaml:"indextts"`
	TemplatesDir string         `yaml:"templates_dir"` // 运行时模板目录；修改 JSON 后可直接重新加载，无需重新编译
	Simulate     bool           `yaml:"simulate"`      // 模拟模式：不连接 ComfyUI，任务按参考耗时模拟执行
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
}

type ComfyInstanceConfig struct {
	GPUIndex int    `yaml:"gpu_index"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Managed  bool   `yaml:"managed"`
}

type ComfyConfig struct {
	ComfyDir        string                `yaml:"comfy_dir"`
	BasePort        int                   `yaml:"base_port"`
	GPUCount        int                   `yaml:"gpu_count"`
	ReserveVRAM     int                   `yaml:"reserve_vram"`
	ForceFP16       bool                  `yaml:"force_fp16"`
	EnableManager   bool                  `yaml:"enable_manager"`
	Mode            string                `yaml:"mode"`             // ssh(默认)/docker/local: 实例调度方式
	ContainerPrefix string                `yaml:"container_prefix"` // docker 模式容器名前缀, 如 comfyui-gpu
	Network         string                `yaml:"network"`          // docker 模式容器网络名, 用于内网解析容器名
	Instances       []ComfyInstanceConfig `yaml:"instances"`        // 可选：跨电脑 ComfyUI 端点；身份为 host+port
}

type StorageConfig struct {
	DBPath  string `yaml:"db_path"`
	DataDir string `yaml:"data_dir"`
}

type GPUConfig struct {
	NVSMICmd   string   `yaml:"nvidia_smi"`
	MonitorInt int      `yaml:"monitor_interval_seconds"`
	BusIDs     []string `yaml:"bus_ids"`
}

type IndexTTSConfig struct {
	Enabled        bool   `yaml:"enabled"`
	ExecutionMode  string `yaml:"execution_mode"` // comfyui（生产多实例）/native_local（显式本机调试）/disabled
	ModelDir       string `yaml:"model_dir"`
	DLLPath        string `yaml:"dll_path"`
	DeviceIndex    int    `yaml:"device_index"`
	Language       string `yaml:"language"`
	DefaultVoice   string `yaml:"default_voice"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	QueueSize      int    `yaml:"queue_size"`
}

// RemoteConfig SSH 远程算力节点配置；Host 为空时按本地模式运行（与旧版本一致）。
type RemoteConfig struct {
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	User          string `yaml:"user"`
	Password      string `yaml:"password"`
	PrivateKey    string `yaml:"private_key"`    // 私钥文件路径（优先于 password）
	KeyPassphrase string `yaml:"key_passphrase"` // 私钥口令（可选）
}

func (r RemoteConfig) Enabled() bool {
	return r.Host != ""
}

func Default() *Config {
	comfyDir, mode := "/opt/comfyUI", "ssh"
	if runtime.GOOS == "windows" {
		comfyDir, mode = `K:\ComfyUI`, "local"
	}
	return &Config{
		Server: ServerConfig{Addr: "0.0.0.0:18000"},
		Comfy: ComfyConfig{
			ComfyDir:        comfyDir,
			BasePort:        8188,
			GPUCount:        8,
			ReserveVRAM:     6,
			ForceFP16:       true,
			Mode:            mode,
			ContainerPrefix: "comfyui-gpu",
			Network:         "comfyui-console_default",
		},
		Storage: StorageConfig{
			DBPath:  "data/console.db",
			DataDir: "data",
		},
		GPU: GPUConfig{
			NVSMICmd:   "nvidia-smi",
			MonitorInt: 3,
		},
		IndexTTS: IndexTTSConfig{
			ExecutionMode:  "native_local",
			Language:       "ZH",
			TimeoutSeconds: 300,
			QueueSize:      32,
		},
	}
}

func executableDir() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err == nil {
		executable = resolved
	}
	return filepath.Dir(executable)
}

func resolveConfigPath() string {
	if explicit := strings.TrimSpace(os.Getenv("COMFYUI_CONSOLE_CONFIG")); explicit != "" {
		if absolute, err := filepath.Abs(explicit); err == nil {
			return absolute
		}
		return filepath.Clean(explicit)
	}
	if dir := executableDir(); dir != "" {
		candidate := filepath.Join(dir, "config.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if absolute, err := filepath.Abs("config.yaml"); err == nil {
		return absolute
	}
	return "config.yaml"
}

func resolveConfiguredPath(value, baseDir string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

func Load() *Config {
	cfg := Default()
	configPath := resolveConfigPath()
	configDir := filepath.Dir(configPath)
	if _, err := os.Stat(configPath); err == nil {
		data, err := os.ReadFile(configPath)
		if err != nil {
			log.Fatalf("read %s: %v", configPath, err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			log.Fatalf("parse %s: %v", configPath, err)
		}
		log.Printf("[config] loaded %s", configPath)
	} else {
		if dir := executableDir(); dir != "" {
			configDir = dir
		}
		log.Printf("[config] %s not found, use default config", configPath)
	}
	cfg.Storage.DBPath = resolveConfiguredPath(cfg.Storage.DBPath, configDir)
	cfg.Storage.DataDir = resolveConfiguredPath(cfg.Storage.DataDir, configDir)
	if cfg.TemplatesDir != "" {
		cfg.TemplatesDir = resolveConfiguredPath(cfg.TemplatesDir, configDir)
	}
	log.Printf("[config] sqlite=%s data_dir=%s", cfg.Storage.DBPath, cfg.Storage.DataDir)
	return cfg
}

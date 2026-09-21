package config

import (
	"log"
	"os"
	"runtime"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig  `yaml:"server"`
	Comfy        ComfyConfig   `yaml:"comfy"`
	Storage      StorageConfig `yaml:"storage"`
	GPU          GPUConfig     `yaml:"gpu"`
	Remote       RemoteConfig  `yaml:"remote"`
	TemplatesDir string        `yaml:"templates_dir"` // 运行时模板目录；修改 JSON 后可直接重新加载，无需重新编译
	Simulate     bool          `yaml:"simulate"`      // 模拟模式：不连接 ComfyUI，任务按参考耗时模拟执行
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
	}
}

func Load() *Config {
	cfg := Default()
	if _, err := os.Stat("config.yaml"); err == nil {
		data, err := os.ReadFile("config.yaml")
		if err != nil {
			log.Fatalf("read config.yaml: %v", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			log.Fatalf("parse config.yaml: %v", err)
		}
	} else {
		log.Printf("[config] config.yaml not found, use default config")
	}
	return cfg
}

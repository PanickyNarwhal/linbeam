package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// PayloadInfo describes discovered local installation media in payloads/ or Downloads.
type PayloadInfo struct {
	PayloadsDir string `json:"payloads_dir"`
	RevuMSI     string `json:"revu_msi"`
	OCRMSI      string `json:"ocr_msi"`
	VCRedistX64 string `json:"vcredist_x64"`
	VCRedistX86 string `json:"vcredist_x86"`
	BPXProfile  string `json:"bpx_profile"`
}

// Config holds the user configuration and state for Linbeam.
type Config struct {
	DataDir          string `json:"data_dir"`
	PrefixDir        string `json:"prefix_dir"`
	RunnersDir       string `json:"runners_dir"`
	CacheDir         string `json:"cache_dir"`
	SelectedRunner   string `json:"selected_runner"`
	PayloadsDir      string `json:"payloads_dir"`
	RevuMSIPath      string `json:"revu_msi_path"`
	OCRMSIPath       string `json:"ocr_msi_path"`
	BPXProfilePath   string `json:"bpx_profile_path"`
	RevuInstalled    bool   `json:"revu_installed"`
	RevuExePath      string `json:"revu_exe_path"`
	FirstRunComplete bool   `json:"first_run_complete"`
}

func configFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "linbeam", "config.json"), nil
}

// DefaultConfig returns a Config populated with standard XDG paths.
func DefaultConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dataDir := filepath.Join(home, ".local", "share", "linbeam")
	cfg := &Config{
		DataDir:    dataDir,
		PrefixDir:  filepath.Join(dataDir, "prefix"),
		RunnersDir: filepath.Join(dataDir, "runners"),
		CacheDir:   filepath.Join(home, ".cache", "linbeam"),
	}
	cfg.AutoDetectPayloads()
	return cfg, nil
}

// Load reads the configuration from disk or returns defaults if not yet saved.
func Load() (*Config, error) {
	cfgPath, err := configFilePath()
	if err != nil {
		return nil, err
	}

	cfg, err := DefaultConfig()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := cfg.EnsureDirs(); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	cfg.AutoDetectPayloads()
	if err := cfg.EnsureDirs(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save writes the current configuration to ~/.config/linbeam/config.json.
func (c *Config) Save() error {
	cfgPath, err := configFilePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cfgPath, data, 0644)
}

// EnsureDirs creates all required Linbeam directories on disk.
func (c *Config) EnsureDirs() error {
	dirs := []string{c.DataDir, c.PrefixDir, c.RunnersDir, c.CacheDir}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}

// AutoDetectPayloads populates RevuMSIPath, OCRMSIPath, and BPXProfilePath if they are empty or missing.
func (c *Config) AutoDetectPayloads() PayloadInfo {
	info := c.DiscoverPayloads()
	if c.PayloadsDir == "" && info.PayloadsDir != "" {
		c.PayloadsDir = info.PayloadsDir
	}
	if (c.RevuMSIPath == "" || !fileExists(c.RevuMSIPath)) && info.RevuMSI != "" {
		c.RevuMSIPath = info.RevuMSI
	}
	if (c.OCRMSIPath == "" || !fileExists(c.OCRMSIPath)) && info.OCRMSI != "" {
		c.OCRMSIPath = info.OCRMSI
	}
	if (c.BPXProfilePath == "" || !fileExists(c.BPXProfilePath)) && info.BPXProfile != "" {
		c.BPXProfilePath = info.BPXProfile
	}
	return info
}

// DiscoverPayloads scans candidate payload directories for Bluebeam Revu 21 media.
func (c *Config) DiscoverPayloads() PayloadInfo {
	var info PayloadInfo

	var candidateDirs []string
	if c.PayloadsDir != "" {
		candidateDirs = append(candidateDirs, c.PayloadsDir)
	}

	if cwd, err := os.Getwd(); err == nil {
		candidateDirs = append(candidateDirs,
			filepath.Join(cwd, "payloads"),
			filepath.Join(filepath.Dir(cwd), "payloads"),
			cwd,
		)
	}

	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidateDirs = append(candidateDirs,
			filepath.Join(exeDir, "payloads"),
			filepath.Join(filepath.Dir(exeDir), "payloads"),
			exeDir,
		)
	}

	if home, err := os.UserHomeDir(); err == nil {
		candidateDirs = append(candidateDirs,
			filepath.Join(home, "linbeam", "payloads"),
			filepath.Join(home, "Downloads"),
		)
	}

	seen := make(map[string]bool)
	for _, dir := range candidateDirs {
		absDir, err := filepath.Abs(dir)
		if err != nil || seen[absDir] {
			continue
		}
		seen[absDir] = true

		entries, err := os.ReadDir(absDir)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			lower := strings.ToLower(name)
			fullPath := filepath.Join(absDir, name)

			if strings.HasSuffix(lower, ".msi") {
				if strings.Contains(lower, "ocr") {
					if info.OCRMSI == "" && !strings.Contains(lower, "patched") {
						info.OCRMSI = fullPath
					}
				} else if strings.Contains(lower, "bluebeam") || strings.Contains(lower, "revu") {
					if info.RevuMSI == "" && !strings.Contains(lower, "patched") {
						info.RevuMSI = fullPath
						if info.PayloadsDir == "" {
							info.PayloadsDir = absDir
						}
					}
				}
			} else if lower == "vc_redist.x64.exe" && info.VCRedistX64 == "" {
				info.VCRedistX64 = fullPath
			} else if lower == "vc_redist.x86.exe" && info.VCRedistX86 == "" {
				info.VCRedistX86 = fullPath
			} else if strings.HasSuffix(lower, ".bpx") && info.BPXProfile == "" {
				info.BPXProfile = fullPath
			}
		}
	}

	if c.RevuMSIPath != "" && fileExists(c.RevuMSIPath) {
		info.RevuMSI = c.RevuMSIPath
	}
	if c.OCRMSIPath != "" && fileExists(c.OCRMSIPath) {
		info.OCRMSI = c.OCRMSIPath
	}
	if c.BPXProfilePath != "" && fileExists(c.BPXProfilePath) {
		info.BPXProfile = c.BPXProfilePath
	}

	return info
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

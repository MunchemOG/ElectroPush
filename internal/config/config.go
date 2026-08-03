package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/spf13/viper"
)

type Profile struct {
	Name     string `mapstructure:"name"`
	SSID     string `mapstructure:"ssid"`
	Password string `mapstructure:"password"`
}

type Config struct {
	DefaultProfile string              `mapstructure:"default_profile"`
	Profiles       map[string]*Profile `mapstructure:"profiles"`
	LastWiFi       string              `mapstructure:"last_wifi"`
	Threads        int                 `mapstructure:"threads"`

	HomeSSID string `mapstructure:"home_ssid"`

	SwitchBack bool `mapstructure:"switch_back"`

	PreferUSB bool `mapstructure:"prefer_usb"`

	AutoSlim bool `mapstructure:"auto_slim"`

	DeltaTransfer bool `mapstructure:"delta_transfer"`

	HubABI string `mapstructure:"hub_abi"`
}

var (
	configDir  string
	configFile string
)

func Initialize() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Under sudo, use the invoking user's home so "pusher" and "sudo pusher"
	// share one config rather than silently diverging.
	if os.Geteuid() == 0 {
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
			if u, err := user.Lookup(sudoUser); err == nil && u != nil && u.HomeDir != "" {
				home = u.HomeDir
			}
		}
	}

	configDir = filepath.Join(home, ".config", "pusher")
	configFile = filepath.Join(configDir, "config.yaml")

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	viper.SetConfigFile(configFile)
	viper.SetConfigType("yaml")

	viper.SetDefault("default_profile", "")
	viper.SetDefault("profiles", map[string]*Profile{})
	viper.SetDefault("last_wifi", "")
	viper.SetDefault("threads", 8)
	viper.SetDefault("home_ssid", "")
	viper.SetDefault("switch_back", true)
	viper.SetDefault("prefer_usb", true)
	viper.SetDefault("auto_slim", false)
	viper.SetDefault("delta_transfer", true)
	viper.SetDefault("hub_abi", "")

	if _, err := os.Stat(configFile); os.IsNotExist(err) {

		if err := viper.WriteConfigAs(configFile); err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}
	} else {

		if err := viper.ReadInConfig(); err != nil {
			return fmt.Errorf("failed to read config file: %w", err)
		}
	}

	return nil
}

func Load() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &cfg, nil
}

func Save(cfg *Config) error {
	viper.Set("default_profile", cfg.DefaultProfile)
	viper.Set("profiles", cfg.Profiles)
	viper.Set("last_wifi", cfg.LastWiFi)
	viper.Set("threads", cfg.Threads)
	viper.Set("home_ssid", cfg.HomeSSID)
	viper.Set("switch_back", cfg.SwitchBack)
	viper.Set("prefer_usb", cfg.PreferUSB)
	viper.Set("auto_slim", cfg.AutoSlim)
	viper.Set("delta_transfer", cfg.DeltaTransfer)
	viper.Set("hub_abi", cfg.HubABI)

	if err := viper.WriteConfig(); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	return nil
}

func AddProfile(name, ssid, password string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}

	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]*Profile)
	}

	cfg.Profiles[name] = &Profile{
		Name:     name,
		SSID:     ssid,
		Password: password,
	}

	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = name
	}

	return Save(cfg)
}

func GetDefaultProfile() (*Profile, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}

	if cfg.DefaultProfile == "" {
		return nil, fmt.Errorf("no default profile set")
	}

	profile, ok := cfg.Profiles[cfg.DefaultProfile]
	if !ok {
		return nil, fmt.Errorf("default profile '%s' not found", cfg.DefaultProfile)
	}

	return profile, nil
}

func SetDefaultProfile(name string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}

	if _, ok := cfg.Profiles[name]; !ok {
		return fmt.Errorf("profile '%s' not found", name)
	}

	cfg.DefaultProfile = name
	return Save(cfg)
}

func SaveLastWiFi(ssid string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}

	cfg.LastWiFi = ssid
	return Save(cfg)
}

func GetLastWiFi() (string, error) {
	cfg, err := Load()
	if err != nil {
		return "", err
	}
	return cfg.LastWiFi, nil
}

func ConfigExists() bool {
	_, err := os.Stat(configFile)
	return err == nil
}

func HasProfiles() (bool, error) {
	cfg, err := Load()
	if err != nil {
		return false, err
	}
	return len(cfg.Profiles) > 0, nil
}

func GetThreads() int {
	threads := viper.GetInt("threads")
	if threads <= 0 {
		return 8
	}
	return threads
}

func SetThreads(count int) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.Threads = count
	return Save(cfg)
}

func ResetThreads() error {
	return SetThreads(8)
}

func GetHomeSSID() string {
	return viper.GetString("home_ssid")
}

func SetHomeSSID(ssid string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.HomeSSID = ssid
	return Save(cfg)
}

func GetSwitchBack() bool {
	return viper.GetBool("switch_back")
}

func SetSwitchBack(enabled bool) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.SwitchBack = enabled
	return Save(cfg)
}

func GetPreferUSB() bool {
	return viper.GetBool("prefer_usb")
}

func SetPreferUSB(enabled bool) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.PreferUSB = enabled
	return Save(cfg)
}

func GetAutoSlim() bool {
	return viper.GetBool("auto_slim")
}

func SetAutoSlim(enabled bool) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.AutoSlim = enabled
	return Save(cfg)
}

func GetDeltaTransfer() bool {
	return viper.GetBool("delta_transfer")
}

func SetDeltaTransfer(enabled bool) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.DeltaTransfer = enabled
	return Save(cfg)
}

func GetHubABI() string {
	return viper.GetString("hub_abi")
}

func SetHubABI(abi string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.HubABI = abi
	return Save(cfg)
}

func DeleteProfile(name string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}

	if _, ok := cfg.Profiles[name]; !ok {
		return fmt.Errorf("profile '%s' not found", name)
	}

	delete(cfg.Profiles, name)

	if cfg.DefaultProfile == name {
		cfg.DefaultProfile = ""
		for remaining := range cfg.Profiles {
			cfg.DefaultProfile = remaining
			break
		}
	}

	return Save(cfg)
}

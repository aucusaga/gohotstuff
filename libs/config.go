package libs

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	Host      string   `yaml:"host,omitempty"`
	Module    string   `yaml:"module,omitempty"`
	Filename  string   `yaml:"filename,omitempty"`
	Address   string   `yaml:"address,omitempty"`
	Bootstrap []string `yaml:"bootstrap,omitempty"`
	Netpath   string   `yaml:"netpath,omitempty"`
	Keypath   string   `yaml:"keypath,omitempty"`

	// TODO: loading WAL instead of configuration
	Round      int      `yaml:"round,omitempty"`
	Startk     string   `yaml:"startk,omitempty"`
	Startv     string   `yaml:"startv,omitempty"`
	Validators []string `yaml:"validators,omitempty"`
}

func GetConfig(cfgFile string) (*Config, error) {
	if cfgFile == "" {
		return defaultConfig(), nil
	}
	return loadConfig(cfgFile)
}

func defaultConfig() *Config {
	return &Config{
		Module:   "gohotstuff",
		Filename: "gohotstuff",
		Address:  "/ip4/127.0.0.1/tcp/30001",

		Round:      0,
		Startk:     "lets_run_hotstuff",
		Startv:     "lets_run_hotstuff_value",
		Validators: []string{},
	}
}

func loadConfig(cfgFile string) (*Config, error) {
	if cfgFile == "" || !FileIsExist(cfgFile) {
		return nil, fmt.Errorf("config file set error.path:%s", cfgFile)
	}

	viperObj := viper.New()
	viperObj.SetConfigFile(cfgFile)
	err := viperObj.ReadInConfig()
	if err != nil {
		return nil, fmt.Errorf("read config failed.path:%s,err:%v", cfgFile, err)
	}

	var config Config
	if err = viperObj.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("unmatshal config failed.path:%s,err:%v", cfgFile, err)
	}

	return &config, nil
}

func FileIsExist(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}

	return true
}

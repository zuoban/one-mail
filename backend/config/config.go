package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	JWT      JWTConfig      `yaml:"jwt"`
	CORS     CORSConfig     `yaml:"cors"`
}

type ServerConfig struct {
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"`
}

type DatabaseConfig struct {
	Type     string `yaml:"type"`
	Path     string `yaml:"path"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
}

type JWTConfig struct {
	Secret  string `yaml:"secret"`
	Expires string `yaml:"expires"`
}

type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
}

var AppConfig *Config

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	AppConfig = &cfg
	return &cfg, nil
}

func GetDSN() string {
	if AppConfig.Database.Type == "sqlite" {
		return AppConfig.Database.Path
	}
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		AppConfig.Database.Host,
		AppConfig.Database.Port,
		AppConfig.Database.User,
		AppConfig.Database.Password,
		AppConfig.Database.DBName,
	)
}

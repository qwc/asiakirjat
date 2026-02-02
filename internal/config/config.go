package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Storage  StorageConfig  `yaml:"storage"`
}

type ServerConfig struct {
	Address string `yaml:"address" env:"ASIAKIRJAT_SERVER_ADDRESS"`
	Port    int    `yaml:"port" env:"ASIAKIRJAT_SERVER_PORT"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver" env:"ASIAKIRJAT_DB_DRIVER"`
	DSN    string `yaml:"dsn" env:"ASIAKIRJAT_DB_DSN"`
}

type AuthConfig struct {
	InitialAdmin InitialAdminConfig `yaml:"initial_admin"`
	Session      SessionConfig      `yaml:"session"`
	LDAP         LDAPConfig         `yaml:"ldap"`
	OAuth2       OAuth2Config       `yaml:"oauth2"`
}

type InitialAdminConfig struct {
	Username string `yaml:"username" env:"ASIAKIRJAT_ADMIN_USERNAME"`
	Password string `yaml:"password" env:"ASIAKIRJAT_ADMIN_PASSWORD"`
}

type SessionConfig struct {
	CookieName string `yaml:"cookie_name" env:"ASIAKIRJAT_SESSION_COOKIE_NAME"`
	MaxAge     int    `yaml:"max_age" env:"ASIAKIRJAT_SESSION_MAX_AGE"`
	Secure     bool   `yaml:"secure" env:"ASIAKIRJAT_SESSION_SECURE"`
}

type LDAPConfig struct {
	Enabled      bool   `yaml:"enabled" env:"ASIAKIRJAT_LDAP_ENABLED"`
	URL          string `yaml:"url" env:"ASIAKIRJAT_LDAP_URL"`
	BindDN       string `yaml:"bind_dn" env:"ASIAKIRJAT_LDAP_BIND_DN"`
	BindPassword string `yaml:"bind_password" env:"ASIAKIRJAT_LDAP_BIND_PASSWORD"`
	BaseDN       string `yaml:"base_dn" env:"ASIAKIRJAT_LDAP_BASE_DN"`
	UserFilter   string `yaml:"user_filter" env:"ASIAKIRJAT_LDAP_USER_FILTER"`
	AdminGroup   string `yaml:"admin_group" env:"ASIAKIRJAT_LDAP_ADMIN_GROUP"`
	EditorGroup  string `yaml:"editor_group" env:"ASIAKIRJAT_LDAP_EDITOR_GROUP"`
}

type OAuth2Config struct {
	Enabled      bool   `yaml:"enabled" env:"ASIAKIRJAT_OAUTH2_ENABLED"`
	ClientID     string `yaml:"client_id" env:"ASIAKIRJAT_OAUTH2_CLIENT_ID"`
	ClientSecret string `yaml:"client_secret" env:"ASIAKIRJAT_OAUTH2_CLIENT_SECRET"`
	AuthURL      string `yaml:"auth_url" env:"ASIAKIRJAT_OAUTH2_AUTH_URL"`
	TokenURL     string `yaml:"token_url" env:"ASIAKIRJAT_OAUTH2_TOKEN_URL"`
	UserInfoURL  string `yaml:"userinfo_url" env:"ASIAKIRJAT_OAUTH2_USERINFO_URL"`
	RedirectURL  string `yaml:"redirect_url" env:"ASIAKIRJAT_OAUTH2_REDIRECT_URL"`
	Scopes       string `yaml:"scopes" env:"ASIAKIRJAT_OAUTH2_SCOPES"`
}

type StorageConfig struct {
	BasePath string `yaml:"base_path" env:"ASIAKIRJAT_STORAGE_PATH"`
}

func Defaults() Config {
	return Config{
		Server: ServerConfig{
			Address: "0.0.0.0",
			Port:    8080,
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			DSN:    "data/asiakirjat.db",
		},
		Auth: AuthConfig{
			InitialAdmin: InitialAdminConfig{
				Username: "admin",
				Password: "admin",
			},
			Session: SessionConfig{
				CookieName: "asiakirjat_session",
				MaxAge:     86400,
				Secure:     false,
			},
		},
		Storage: StorageConfig{
			BasePath: "data/projects",
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Defaults()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("reading config file: %w", err)
			}
			// Config file not found — continue with defaults
		} else {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return nil, fmt.Errorf("parsing config file: %w", err)
			}
		}
	}

	applyEnvOverrides(&cfg)
	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	applyEnvToStruct(reflect.ValueOf(cfg).Elem())
}

func applyEnvToStruct(v reflect.Value) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldVal := v.Field(i)

		if fieldVal.Kind() == reflect.Struct {
			applyEnvToStruct(fieldVal)
			continue
		}

		envTag := field.Tag.Get("env")
		if envTag == "" {
			continue
		}

		envVal, ok := os.LookupEnv(envTag)
		if !ok {
			continue
		}

		switch fieldVal.Kind() {
		case reflect.String:
			fieldVal.SetString(envVal)
		case reflect.Int:
			if n, err := strconv.Atoi(envVal); err == nil {
				fieldVal.SetInt(int64(n))
			}
		case reflect.Bool:
			fieldVal.SetBool(strings.EqualFold(envVal, "true") || envVal == "1")
		}
	}
}

func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Server.Address, c.Server.Port)
}

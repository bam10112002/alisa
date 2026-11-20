package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// Config contains application settings loaded from environment variables.
type Config struct {
	HTTPAddr        string
	ShutdownTimeout time.Duration
	TickTickToken   string
	TickTickTimeout time.Duration
	GigaChatAuthKey string
	GigaChatScope   string
	GigaChatModel   string
	GigaChatBaseURL string
	GigaChatAuthURL string
	GigaChatCAFile  string
	GigaChatTimeout time.Duration
}

func Load() (Config, error) {
	if err := godotenv.Load(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Parser errors can contain secrets from the offending line.
		return Config{}, errors.New("cannot load .env: check file permissions and dotenv syntax")
	}
	cfg := Config{
		HTTPAddr:        ":8080",
		ShutdownTimeout: 10 * time.Second,
		TickTickToken:   os.Getenv("TICKTICK_ACCESS_TOKEN"),
		TickTickTimeout: 10 * time.Second,
		GigaChatAuthKey: os.Getenv("GIGACHAT_AUTH_KEY"),
		GigaChatScope:   "GIGACHAT_API_PERS",
		GigaChatModel:   "GigaChat-2-Pro",
		GigaChatBaseURL: "https://api.giga.chat/v1",
		GigaChatAuthURL: "https://ngw.devices.sberbank.ru:9443/api/v2/oauth",
		GigaChatCAFile:  os.Getenv("GIGACHAT_CA_FILE"),
		GigaChatTimeout: 60 * time.Second,
	}
	if value := os.Getenv("HTTP_ADDR"); value != "" {
		cfg.HTTPAddr = value
	}
	if value := os.Getenv("SHUTDOWN_TIMEOUT"); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT must be a positive duration, got %q", value)
		}
		cfg.ShutdownTimeout = duration
	}
	if value := os.Getenv("TICKTICK_TIMEOUT"); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return Config{}, fmt.Errorf("TICKTICK_TIMEOUT must be a positive duration, got %q", value)
		}
		cfg.TickTickTimeout = duration
	}
	for key, target := range map[string]*string{
		"GIGACHAT_SCOPE":    &cfg.GigaChatScope,
		"GIGACHAT_MODEL":    &cfg.GigaChatModel,
		"GIGACHAT_BASE_URL": &cfg.GigaChatBaseURL,
		"GIGACHAT_AUTH_URL": &cfg.GigaChatAuthURL,
	} {
		if value := os.Getenv(key); value != "" {
			*target = value
		}
	}
	if value := os.Getenv("GIGACHAT_TIMEOUT"); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return Config{}, fmt.Errorf("GIGACHAT_TIMEOUT must be a positive duration, got %q", value)
		}
		cfg.GigaChatTimeout = duration
	}
	return cfg, nil
}

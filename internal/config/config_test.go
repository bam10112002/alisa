package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadDotEnv(t *testing.T) {
	for _, tc := range []struct {
		name      string
		contents  string
		missing   bool
		override  bool
		wantError bool
	}{
		{name: "quoted token", contents: "TICKTICK_ACCESS_TOKEN='test-token'\n"},
		{name: "environment wins", contents: "TICKTICK_ACCESS_TOKEN=file-token\n", override: true},
		{name: "missing file", missing: true},
		{name: "invalid syntax", contents: "INVALID!KEY=private-secret\n", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			for _, key := range []string{"HTTP_ADDR", "SHUTDOWN_TIMEOUT", "TICKTICK_TIMEOUT", "TICKTICK_ACCESS_TOKEN", "GIGACHAT_AUTH_KEY", "GIGACHAT_SCOPE", "GIGACHAT_MODEL", "GIGACHAT_BASE_URL", "GIGACHAT_AUTH_URL", "GIGACHAT_CA_FILE", "GIGACHAT_TIMEOUT"} {
				t.Setenv(key, "") // Register restoration of the original environment.
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			if tc.override {
				t.Setenv("TICKTICK_ACCESS_TOKEN", "environment-token")
			}
			if !tc.missing {
				if err := os.WriteFile(".env", []byte(tc.contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load()
			if tc.wantError {
				if err == nil || strings.Contains(err.Error(), "private-secret") {
					t.Fatalf("unsafe or missing error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := "test-token"
			if tc.override {
				want = "environment-token"
			}
			if tc.missing {
				want = ""
			}
			if cfg.TickTickToken != want {
				t.Fatal("unexpected token source")
			}
		})
	}
}

func TestGigaChatConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, key := range []string{"HTTP_ADDR", "SHUTDOWN_TIMEOUT", "TICKTICK_TIMEOUT", "GIGACHAT_AUTH_KEY", "GIGACHAT_SCOPE", "GIGACHAT_MODEL", "GIGACHAT_BASE_URL", "GIGACHAT_AUTH_URL", "GIGACHAT_CA_FILE", "GIGACHAT_TIMEOUT"} {
		t.Setenv(key, "")
	}
	cfg, err := Load()
	if err != nil || cfg.GigaChatTimeout != 60*time.Second || cfg.GigaChatModel != "GigaChat-2-Pro" || cfg.GigaChatScope != "GIGACHAT_API_PERS" || cfg.GigaChatBaseURL != "https://api.giga.chat/v1" {
		t.Fatalf("unexpected defaults or error: %v", err)
	}
	t.Setenv("GIGACHAT_AUTH_KEY", "test-key")
	t.Setenv("GIGACHAT_SCOPE", "GIGACHAT_API_CORP")
	t.Setenv("GIGACHAT_MODEL", "custom-model")
	t.Setenv("GIGACHAT_BASE_URL", "https://api.example.com/v1")
	t.Setenv("GIGACHAT_AUTH_URL", "https://auth.example.com/token")
	t.Setenv("GIGACHAT_CA_FILE", "ca.pem")
	t.Setenv("GIGACHAT_TIMEOUT", "2m")
	cfg, err = Load()
	if err != nil || cfg.GigaChatAuthKey != "test-key" || cfg.GigaChatScope != "GIGACHAT_API_CORP" || cfg.GigaChatModel != "custom-model" || cfg.GigaChatTimeout != 2*time.Minute || cfg.GigaChatBaseURL != "https://api.example.com/v1" || cfg.GigaChatAuthURL != "https://auth.example.com/token" || cfg.GigaChatCAFile != "ca.pem" {
		t.Fatal("configuration overrides lost")
	}
	for _, value := range []string{"0s", "-1s", "invalid"} {
		t.Setenv("GIGACHAT_TIMEOUT", value)
		if _, err := Load(); err == nil {
			t.Fatalf("invalid timeout accepted: %q", value)
		}
	}
}

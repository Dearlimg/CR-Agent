package logic

import (
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port            string
	SkillsDir       string
	DeepSeekAPIKey  string
	DeepSeekBaseURL string
	MySQLDSN        string
	RedisAddr       string
	RedisPassword   string
	BudgetCents     int
}

func LoadConfig() Config {
	_ = godotenv.Load()
	b := func(k, d string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return d
	}
	n, _ := strconv.Atoi(b("REVIEW_BUDGET_CENTS", "1000"))
	return Config{Port: b("PORT", "8080"), SkillsDir: b("AGENT_SKILLS_DIR", "skills"), DeepSeekAPIKey: b("DEEPSEEK_API_KEY", ""), DeepSeekBaseURL: b("DEEPSEEK_BASE_URL", "https://api.deepseek.com"), MySQLDSN: b("MYSQL_DSN", ""), RedisAddr: b("REDIS_ADDR", "127.0.0.1:6379"), RedisPassword: b("REDIS_PASSWORD", ""), BudgetCents: n}
}
func redact(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		low := strings.ToLower(l)
		if strings.Contains(low, "api_key") || strings.Contains(low, "apikey") || strings.Contains(low, "password") || strings.Contains(low, "secret") || strings.Contains(low, "authorization") {
			lines[i] = "[REDACTED]"
		}
	}
	return strings.Join(lines, "\n")
}
func jsonString(v any) string { b, _ := json.Marshal(v); return string(b) }
func envExample()             { _ = fmt.Sprintf("") }

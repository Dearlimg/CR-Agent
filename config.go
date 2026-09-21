package main

import (
 "encoding/json"
 "fmt"
 "os"
 "strconv"
 "strings"
)

type Config struct { Port string; DeepSeekAPIKey string; DeepSeekBaseURL string; MySQLDSN string; RedisAddr string; RedisPassword string; BudgetCents int }
func loadConfig() Config { b:=func(k,d string) string { if v:=os.Getenv(k); v!="" { return v }; return d }; n,_:=strconv.Atoi(b("REVIEW_BUDGET_CENTS","1000")); return Config{Port:b("PORT","8080"),DeepSeekAPIKey:b("DEEPSEEK_API_KEY",""),DeepSeekBaseURL:b("DEEPSEEK_BASE_URL","https://api.deepseek.com"),MySQLDSN:b("MYSQL_DSN","root:sta_go@tcp(121.40.235.227:3307)/cr_agent?charset=utf8mb4&parseTime=True"),RedisAddr:b("REDIS_ADDR","121.40.235.227:6379"),RedisPassword:b("REDIS_PASSWORD","sta_go"),BudgetCents:n} }
func redact(s string) string { lines:=strings.Split(s,"\n"); for i,l:=range lines { low:=strings.ToLower(l); if strings.Contains(low,"api_key")||strings.Contains(low,"apikey")||strings.Contains(low,"password")||strings.Contains(low,"secret")||strings.Contains(low,"authorization") { lines[i]="[REDACTED]" } }; return strings.Join(lines,"\n") }
func jsonString(v any) string { b,_:=json.Marshal(v); return string(b) }
func envExample() { _=fmt.Sprintf("") }

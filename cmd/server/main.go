package main

import (
	"CR-Agent/internal/controller"
	"CR-Agent/internal/dao"
	"CR-Agent/internal/logic"
	"github.com/gin-gonic/gin"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	svc := logic.NewService(dao.NewJobStore(".checkpoints"), logic.Config{Port: port, DeepSeekAPIKey: os.Getenv("DEEPSEEK_API_KEY"), DeepSeekBaseURL: os.Getenv("DEEPSEEK_BASE_URL")})
	r := gin.Default()
	r.StaticFile("/", "web/index.html")
	controller.NewReviewController(svc).Register(r)
	_ = r.Run(":" + port)
}

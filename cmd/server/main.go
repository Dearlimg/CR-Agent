package main

import (
	"CR-Agent/internal/controller"
	"CR-Agent/internal/dao"
	"CR-Agent/internal/logic"
	"github.com/gin-gonic/gin"
)

func main() {
	cfg := logic.LoadConfig()
	svc := logic.NewService(dao.NewJobStore(".checkpoints"), cfg)
	r := gin.Default()
	r.StaticFile("/", "web/index.html")
	controller.NewReviewController(svc).Register(r)
	_ = r.Run(":" + cfg.Port)
}

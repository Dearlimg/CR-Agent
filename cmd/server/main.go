package main

import (
	"CR-Agent/internal/controller"
	"CR-Agent/internal/dao"
	"CR-Agent/internal/hooks"
	"CR-Agent/internal/logic"
	"github.com/gin-gonic/gin"
	"os"
)

func main() {
	cfg := logic.LoadConfig()
	store := dao.Store(dao.NewJobStore(".checkpoints"))
	if cfg.MySQLDSN != "" {
		mysqlStore, err := dao.OpenMySQL(cfg.MySQLDSN)
		if err != nil {
			panic(err)
		}
		if os.Getenv("AUTO_MIGRATE") != "false" {
			if err := mysqlStore.Migrate(); err != nil {
				panic(err)
			}
		}
		store = mysqlStore
	}
	svc := logic.NewService(store, cfg)
	hooks.RegisterAudit(svc.Loop.Hooks)
	r := gin.Default()
	r.StaticFile("/", "web/index.html")
	controller.NewReviewController(svc).Register(r)
	_ = r.Run(":" + cfg.Port)
}

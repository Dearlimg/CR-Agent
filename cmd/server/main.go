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
	if cfg.PersistenceMode != "mysql" {
		panic("生产服务只支持 mysql 持久化，PERSISTENCE_MODE 必须为 mysql")
	}
	if cfg.MySQLDSN == "" {
		panic("MYSQL_DSN 不能为空，服务不会回退到本地文件存储")
	}
	mysqlStore, err := dao.OpenMySQL(cfg.MySQLDSN)
	if err != nil {
		panic(err)
	}
	if os.Getenv("AUTO_MIGRATE") != "false" {
		if err := mysqlStore.Migrate(); err != nil {
			panic(err)
		}
	}
	background, err := dao.NewMySQLBackgroundRepository(mysqlStore.DB())
	if err != nil {
		panic(err)
	}
	mailbox, err := dao.NewMySQLTeamMailbox(mysqlStore.DB())
	if err != nil {
		panic(err)
	}
	svc := logic.NewServiceWithRuntime(mysqlStore, cfg, logic.RuntimeRepositories{
		Tasks:      dao.NewMySQLTaskRepository(mysqlStore.DB()),
		Background: background,
		Mailbox:    mailbox,
	})
	if err := svc.Start(); err != nil {
		panic(err)
	}
	defer svc.Stop()
	hooks.RegisterAudit(svc.Loop.Hooks)
	r := gin.Default()
	r.StaticFile("/", "web/index.html")
	controller.NewReviewController(svc).Register(r)
	_ = r.Run(":" + cfg.Port)
}

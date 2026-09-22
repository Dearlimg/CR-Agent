package controller

import (
	"CR-Agent/internal/logic"
	"CR-Agent/internal/model"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

type ReviewController struct{ Service *logic.Service }

func NewReviewController(s *logic.Service) *ReviewController { return &ReviewController{Service: s} }
func (c *ReviewController) Register(r *gin.Engine) {
	r.GET("/api/health", func(x *gin.Context) { x.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.POST("/api/reviews", c.create)
	r.GET("/api/reviews/:id", c.get)
}
func (c *ReviewController) create(x *gin.Context) {
	var req model.ReviewRequest
	if x.ShouldBindJSON(&req) != nil || (strings.TrimSpace(req.Source) == "" && strings.TrimSpace(req.Diff) == "") {
		x.JSON(400, gin.H{"error": "source 或 diff 至少填写一项"})
		return
	}
	j, err := c.Service.Create(req)
	if err != nil {
		x.JSON(500, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusAccepted, j)
}
func (c *ReviewController) get(x *gin.Context) {
	j, ok := c.Service.Store.Get(x.Param("id"))
	if !ok {
		x.JSON(404, gin.H{"error": "review 不存在"})
		return
	}
	x.JSON(200, j)
}

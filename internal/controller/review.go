package controller

import (
	"CR-Agent/internal/logic"
	"CR-Agent/internal/model"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"time"
)

type ReviewController struct{ Service *logic.Service }

func NewReviewController(s *logic.Service) *ReviewController { return &ReviewController{Service: s} }
func (c *ReviewController) Register(r *gin.Engine) {
	r.GET("/api/health", func(x *gin.Context) { x.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.POST("/api/reviews", c.create)
	r.GET("/api/reviews/:id", c.get)
	r.GET("/api/reviews/:id/events", c.events)
	r.GET("/api/memories", c.listMemories)
	r.POST("/api/memories", c.saveMemory)
}

func (c *ReviewController) listMemories(x *gin.Context) {
	records, err := c.Service.MemoryStore.List()
	if err != nil {
		x.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusOK, gin.H{"memories": records})
}

func (c *ReviewController) saveMemory(x *gin.Context) {
	var candidate logic.MemoryCandidate
	if err := x.ShouldBindJSON(&candidate); err != nil {
		x.JSON(http.StatusBadRequest, gin.H{"error": "记忆格式无效"})
		return
	}
	record, saved, err := c.Service.MemoryStore.Save(candidate)
	if err != nil {
		x.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !saved {
		x.JSON(http.StatusBadRequest, gin.H{"error": "仅保存非重复的 persistent 长期记忆"})
		return
	}
	x.JSON(http.StatusCreated, record)
}

func (c *ReviewController) events(x *gin.Context) {
	x.Header("Content-Type", "text/event-stream")
	x.Header("Cache-Control", "no-cache")
	x.Header("Connection", "keep-alive")
	flusher, ok := x.Writer.(http.Flusher)
	if !ok {
		x.Status(http.StatusInternalServerError)
		return
	}
	for {
		j, found := c.Service.Store.Get(x.Param("id"))
		if !found {
			x.SSEvent("error", gin.H{"error": "review 不存在"})
			flusher.Flush()
			return
		}
		x.SSEvent("review", j)
		flusher.Flush()
		if j.Status == "completed" || j.Status == "failed" {
			return
		}
		select {
		case <-x.Request.Context().Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
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

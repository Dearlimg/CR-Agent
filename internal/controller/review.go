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
	r.POST("/api/tasks", c.createTask)
	r.GET("/api/tasks", c.listTasks)
	r.GET("/api/tasks/:id", c.getTask)
	r.PATCH("/api/tasks/:id/dependencies", c.addTaskDependencies)
	r.POST("/api/tasks/:id/claim", c.claimTask)
	r.POST("/api/tasks/:id/complete", c.completeTask)
	r.GET("/api/background-tasks", c.listBackgroundTasks)
	r.GET("/api/background-tasks/notifications", c.collectBackgroundNotifications)
	r.GET("/api/background-tasks/:id", c.getBackgroundTask)
	r.POST("/api/background-tasks/:id/cancel", c.cancelBackgroundTask)
	r.POST("/api/cron-jobs", c.createCronJob)
	r.GET("/api/cron-jobs", c.listCronJobs)
	r.GET("/api/cron-jobs/:id", c.getCronJob)
	r.DELETE("/api/cron-jobs/:id", c.cancelCronJob)
}

type createCronJobRequest struct {
	Cron        string `json:"cron"`
	Source      string `json:"source"`
	MemoryQuery string `json:"memory_query"`
	Recurring   bool   `json:"recurring"`
	Durable     bool   `json:"durable"`
}

func (c *ReviewController) createCronJob(x *gin.Context) {
	if c.Service.CronError != nil || c.Service.Cron == nil {
		x.JSON(http.StatusServiceUnavailable, gin.H{"error": "定时调度器不可用"})
		return
	}
	var req createCronJobRequest
	if err := x.ShouldBindJSON(&req); err != nil {
		x.JSON(http.StatusBadRequest, gin.H{"error": "定时任务格式无效"})
		return
	}
	job, err := c.Service.Cron.Schedule(req.Cron, req.Source, req.MemoryQuery, req.Recurring, req.Durable)
	if err != nil {
		x.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusCreated, job)
}

func (c *ReviewController) listCronJobs(x *gin.Context) {
	if c.Service.CronError != nil || c.Service.Cron == nil {
		x.JSON(http.StatusServiceUnavailable, gin.H{"error": "定时调度器不可用"})
		return
	}
	x.JSON(http.StatusOK, gin.H{"cron_jobs": c.Service.Cron.List()})
}

func (c *ReviewController) getCronJob(x *gin.Context) {
	if c.Service.CronError != nil || c.Service.Cron == nil {
		x.JSON(http.StatusServiceUnavailable, gin.H{"error": "定时调度器不可用"})
		return
	}
	job, ok := c.Service.Cron.Get(x.Param("id"))
	if !ok {
		x.JSON(http.StatusNotFound, gin.H{"error": "定时任务不存在"})
		return
	}
	x.JSON(http.StatusOK, job)
}

func (c *ReviewController) cancelCronJob(x *gin.Context) {
	if c.Service.CronError != nil || c.Service.Cron == nil {
		x.JSON(http.StatusServiceUnavailable, gin.H{"error": "定时调度器不可用"})
		return
	}
	job, err := c.Service.Cron.Cancel(x.Param("id"))
	if err != nil {
		x.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusOK, job)
}

func (c *ReviewController) listBackgroundTasks(x *gin.Context) {
	tasks, err := c.Service.Background.List()
	if err != nil {
		x.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

func (c *ReviewController) collectBackgroundNotifications(x *gin.Context) {
	tasks, err := c.Service.Background.Collect()
	if err != nil {
		x.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusOK, gin.H{"notifications": tasks})
}

func (c *ReviewController) getBackgroundTask(x *gin.Context) {
	task, err := c.Service.Background.Get(x.Param("id"))
	if err != nil {
		x.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusOK, task)
}

func (c *ReviewController) cancelBackgroundTask(x *gin.Context) {
	task, err := c.Service.Background.Cancel(x.Param("id"))
	if err != nil {
		x.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusOK, task)
}

type createTaskRequest struct {
	Subject     string `json:"subject"`
	Description string `json:"description"`
}

type taskDependenciesRequest struct {
	BlockedBy []string `json:"blocked_by"`
}

type taskOwnerRequest struct {
	Owner string `json:"owner"`
}

func (c *ReviewController) createTask(x *gin.Context) {
	var req createTaskRequest
	if err := x.ShouldBindJSON(&req); err != nil {
		x.JSON(http.StatusBadRequest, gin.H{"error": "任务格式无效"})
		return
	}
	task, err := c.Service.TaskStore.Create(req.Subject, req.Description)
	if err != nil {
		x.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusCreated, task)
}

func (c *ReviewController) listTasks(x *gin.Context) {
	tasks, err := c.Service.TaskStore.List()
	if err != nil {
		x.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

func (c *ReviewController) getTask(x *gin.Context) {
	task, err := c.Service.TaskStore.Get(x.Param("id"))
	if err != nil {
		x.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusOK, task)
}

func (c *ReviewController) addTaskDependencies(x *gin.Context) {
	var req taskDependenciesRequest
	if err := x.ShouldBindJSON(&req); err != nil {
		x.JSON(http.StatusBadRequest, gin.H{"error": "依赖格式无效"})
		return
	}
	task, err := c.Service.TaskStore.AddDependencies(x.Param("id"), req.BlockedBy)
	if err != nil {
		x.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusOK, task)
}

func (c *ReviewController) claimTask(x *gin.Context) {
	var req taskOwnerRequest
	_ = x.ShouldBindJSON(&req)
	if req.Owner == "" {
		req.Owner = "agent"
	}
	task, err := c.Service.TaskStore.Claim(x.Param("id"), req.Owner)
	if err != nil {
		x.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusOK, task)
}

func (c *ReviewController) completeTask(x *gin.Context) {
	var req taskOwnerRequest
	_ = x.ShouldBindJSON(&req)
	if req.Owner == "" {
		req.Owner = "agent"
	}
	task, unblocked, err := c.Service.TaskStore.Complete(x.Param("id"), req.Owner)
	if err != nil {
		x.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	x.JSON(http.StatusOK, gin.H{"task": task, "unblocked": unblocked})
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
		terminal := reviewTerminal(j.Status)
		if !terminal || j.FinishedAt != nil {
			x.SSEvent("review", j)
			flusher.Flush()
			if terminal {
				return
			}
		}
		select {
		case <-x.Request.Context().Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func reviewTerminal(status string) bool {
	switch status {
	case "completed", "completed_with_warnings", "failed", "cancelled":
		return true
	default:
		return false
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

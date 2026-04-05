package watcher

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// Handler handles watch directory HTTP operations
type Handler struct {
	service *Service
}

// NewHandler creates a new watcher handler
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers watcher routes
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	watch := r.Group("/watch")

	// Directories
	dirs := watch.Group("/directories")
	dirs.GET("", h.List)
	dirs.POST("", h.Add)
	dirs.GET("/:id", h.Get)
	dirs.PUT("/:id", h.Update)
	dirs.DELETE("/:id", h.Remove)

	// Watcher control
	watch.POST("/start", h.Start)
	watch.POST("/stop", h.Stop)
	watch.GET("/status", h.Status)
}

// parseID helper to parse int64 id from URL param
func parseID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}

// List GET /api/v1/watch/directories
func (h *Handler) List(c *gin.Context) {
	dirs, err := h.service.List(c.Request.Context())
	if err != nil {
		logrus.WithError(err).Error("failed to list watch directories")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list watch directories"})
		return
	}

	c.JSON(http.StatusOK, ListResponse{Directories: dirs})
}

// Add POST /api/v1/watch/directories
func (h *Handler) Add(c *gin.Context) {
	var req AddRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dir, err := h.service.Add(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, ErrAlreadyExists):
			c.JSON(http.StatusConflict, gin.H{"error": "watch directory already exists"})
		case errors.Is(err, ErrNestedPath):
			c.JSON(http.StatusConflict, gin.H{"error": "path is nested within an existing recursive watch directory"})
		case errors.Is(err, ErrParentPathExists):
			c.JSON(http.StatusConflict, gin.H{"error": "a recursive watch directory already covers this path"})
		case errors.Is(err, ErrPathDoesNotExist):
			c.JSON(http.StatusBadRequest, gin.H{"error": "path does not exist"})
		default:
			logrus.WithError(err).Error("failed to add watch directory")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add watch directory"})
		}
		return
	}

	c.JSON(http.StatusCreated, dir)
}

// GET & PUT operations for a watch directory, relevant fields include
// pattern, recursive, priority

// Get GET /api/v1/watch/directories/:id
func (h *Handler) Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	dir, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "watch directory not found"})
			return
		}
		logrus.WithError(err).Error("failed to get watch directory")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get watch directory"})
		return
	}

	c.JSON(http.StatusOK, dir)
}

// Update PUT /api/v1/watch/directories/:id
func (h *Handler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req UpdateRequest
	err = c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dir, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "watch directory not found"})
			return
		}
		logrus.WithError(err).Error("failed to update watch directory")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update watch directory"})
		return
	}

	c.JSON(http.StatusOK, dir)
}

// Remove DELETE /api/v1/watch/directories/:id
func (h *Handler) Remove(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	err = h.service.Remove(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "watch directory not found"})
			return
		}
		logrus.WithError(err).Error("failed to remove watch directory")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to remove watch directory"})
		return
	}

	c.Status(http.StatusNoContent)
}

// Start POST /api/v1/watch/start
func (h *Handler) Start(c *gin.Context) {
	err := h.service.StartWatcher(c.Request.Context())
	if err != nil {
		logrus.WithError(err).Error("failed to start file watcher")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start file watcher"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "started", "running": true})
}

// Stop POST /api/v1/watch/stop
func (h *Handler) Stop(c *gin.Context) {
	err := h.service.StopWatcher()
	if err != nil {
		logrus.WithError(err).Error("failed to stop file watcher")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to stop file watcher"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "stopped", "running": false})
}

// Status GET /api/v1/watch/status
func (h *Handler) Status(c *gin.Context) {
	running := h.service.IsWatcherRunning()
	c.JSON(http.StatusOK, gin.H{"running": running})
}

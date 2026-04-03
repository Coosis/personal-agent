package conversations

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// Handler handles conversation HTTP operations
type Handler struct {
	service *Service
}

// NewHandler creates a new conversation handler
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers conversation routes
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	convs := r.Group("/conversations")
	convs.GET("", h.List)
	convs.POST("", h.Create)
	convs.GET("/:id", h.Get)
	convs.PUT("/:id", h.Update)
	convs.DELETE("/:id", h.Delete)
	convs.POST("/:id/messages", h.SendMessage)
	convs.GET("/:id/messages", h.ListMessages)
}

// parseID helper to parse int64 id from URL param
func parseID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}

// List GET /api/v1/conversations
func (h *Handler) List(c *gin.Context) {
	var req ListRequest
	err := c.ShouldBindQuery(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	convs, err := h.service.List(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to list conversations")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list conversations"})
		return
	}

	c.JSON(http.StatusOK, ListResponse{Conversations: convs})
}

// Get GET /api/v1/conversations/:id
func (h *Handler) Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	conv, msgs, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
			return
		}
		logrus.WithError(err).Error("failed to get conversation")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get conversation"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"conversation": conv,
		"messages":     msgs,
	})
}

// Create POST /api/v1/conversations
func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	conv, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to create conversation")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create conversation"})
		return
	}

	c.JSON(http.StatusCreated, conv)
}

// Update PUT /api/v1/conversations/:id
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

	conv, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
			return
		}
		logrus.WithError(err).Error("failed to update conversation")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update conversation"})
		return
	}

	c.JSON(http.StatusOK, conv)
}

// Delete DELETE /api/v1/conversations/:id
func (h *Handler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	err = h.service.Delete(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
			return
		}
		logrus.WithError(err).Error("failed to delete conversation")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete conversation"})
		return
	}

	c.Status(http.StatusNoContent)
}

// SendMessage POST /api/v1/conversations/:id/messages
func (h *Handler) SendMessage(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var req SendMessageRequest
	err = c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	msg, err := h.service.SendMessage(c.Request.Context(), id, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
			return
		}
		logrus.WithError(err).Error("failed to send message")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send message"})
		return
	}

	c.JSON(http.StatusCreated, msg)
}

// ListMessages GET /api/v1/conversations/:id/messages
func (h *Handler) ListMessages(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	_, msgs, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
			return
		}
		logrus.WithError(err).Error("failed to list messages")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list messages"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"messages": msgs})
}

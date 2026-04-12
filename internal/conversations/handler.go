package conversations

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/Coosis/personal-agent/internal/apiutil"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	group := r.Group("/conversations")
	group.POST("", h.Create)
	group.GET("", h.List)
	group.GET("/:id", h.Get)
	group.POST("/:id/messages", h.SendMessage)
	group.GET("/:id/messages", h.ListMessages)
	group.POST("/:id/messages/stream", h.StreamMessage) // check stream.go
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	item, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to create conversation")
		apiutil.Error(c, http.StatusInternalServerError, "failed to create conversation")
		return
	}

	c.JSON(http.StatusCreated, item)
}

func (h *Handler) List(c *gin.Context) {
	var req ListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	items, err := h.service.List(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to list conversations")
		apiutil.Error(c, http.StatusInternalServerError, "failed to list conversations")
		return
	}

	c.JSON(http.StatusOK, ListResponse{Conversations: items})
}

func (h *Handler) Get(c *gin.Context) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiutil.Error(c, http.StatusNotFound, "conversation not found")
			return
		}
		logrus.WithError(err).Error("failed to get conversation")
		apiutil.Error(c, http.StatusInternalServerError, "failed to get conversation")
		return
	}

	c.JSON(http.StatusOK, item)
}

func (h *Handler) SendMessage(c *gin.Context) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	var req SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.service.SendMessage(c.Request.Context(), id, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			apiutil.Error(c, http.StatusNotFound, "conversation not found")
		case errors.Is(err, ErrAgentUnavailable):
			apiutil.Error(c, http.StatusBadGateway, "agent unavailable")
		default:
			logrus.WithError(err).Error("failed to send message")
			apiutil.Error(c, http.StatusInternalServerError, "failed to send message")
		}
		return
	}

	c.JSON(http.StatusCreated, result)
}

func (h *Handler) ListMessages(c *gin.Context) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	var req ListMessagesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	items, err := h.service.ListMessages(c.Request.Context(), id, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiutil.Error(c, http.StatusNotFound, "conversation not found")
			return
		}
		logrus.WithError(err).Error("failed to list messages")
		apiutil.Error(c, http.StatusInternalServerError, "failed to list messages")
		return
	}

	c.JSON(http.StatusOK, gin.H{"messages": items})
}

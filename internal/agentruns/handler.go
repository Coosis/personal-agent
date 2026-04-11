package agentruns

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
	group := r.Group("/agent-runs")
	group.GET("", h.List)
	group.GET("/:id", h.Get)
}

func (h *Handler) List(c *gin.Context) {
	var req ListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	items, err := h.service.List(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to list agent runs")
		apiutil.Error(c, http.StatusInternalServerError, "failed to list agent runs")
		return
	}

	c.JSON(http.StatusOK, ListResponse{AgentRuns: items})
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
			apiutil.Error(c, http.StatusNotFound, "agent run not found")
			return
		}
		logrus.WithError(err).Error("failed to get agent run")
		apiutil.Error(c, http.StatusInternalServerError, "failed to get agent run")
		return
	}

	c.JSON(http.StatusOK, item)
}

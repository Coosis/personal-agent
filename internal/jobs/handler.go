package jobs

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
	group := r.Group("/jobs")
	group.GET("", h.List)
	group.GET("/:id", h.Get)
	group.POST("/:id/retry", h.Retry)
}

func (h *Handler) List(c *gin.Context) {
	var req ListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	items, err := h.service.List(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to list jobs")
		apiutil.Error(c, http.StatusInternalServerError, "failed to list jobs")
		return
	}

	c.JSON(http.StatusOK, ListResponse{Jobs: items})
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
			apiutil.Error(c, http.StatusNotFound, "job not found")
			return
		}
		logrus.WithError(err).Error("failed to get job")
		apiutil.Error(c, http.StatusInternalServerError, "failed to get job")
		return
	}

	c.JSON(http.StatusOK, item)
}

func (h *Handler) Retry(c *gin.Context) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	item, err := h.service.Retry(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiutil.Error(c, http.StatusNotFound, "job not found")
			return
		}
		logrus.WithError(err).Error("failed to retry job")
		apiutil.Error(c, http.StatusInternalServerError, "failed to retry job")
		return
	}

	c.JSON(http.StatusOK, item)
}

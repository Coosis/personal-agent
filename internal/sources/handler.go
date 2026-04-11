package sources

import (
	"context"
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
	group := r.Group("/sources")
	group.POST("", h.Create)
	group.GET("", h.List)
	group.GET("/:id", h.Get)
	group.DELETE("/:id", h.Delete)
	group.POST("/:id/scan", h.Scan)
	group.POST("/:id/purge", h.Purge)
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	item, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidType):
			apiutil.Error(c, http.StatusBadRequest, "source type must be file or directory")
		default:
			logrus.WithError(err).Error("failed to create source")
			apiutil.Error(c, http.StatusInternalServerError, "failed to create source")
		}
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
		logrus.WithError(err).Error("failed to list sources")
		apiutil.Error(c, http.StatusInternalServerError, "failed to list sources")
		return
	}

	c.JSON(http.StatusOK, ListResponse{Sources: items})
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
			apiutil.Error(c, http.StatusNotFound, "source not found")
			return
		}
		logrus.WithError(err).Error("failed to get source")
		apiutil.Error(c, http.StatusInternalServerError, "failed to get source")
		return
	}

	c.JSON(http.StatusOK, item)
}

func (h *Handler) Delete(c *gin.Context) {
	h.lifecycle(c, h.service.Delete, http.StatusOK)
}

func (h *Handler) Scan(c *gin.Context) {
	h.lifecycle(c, h.service.Scan, http.StatusAccepted)
}

func (h *Handler) Purge(c *gin.Context) {
	h.lifecycle(c, h.service.Purge, http.StatusAccepted)
}

func (h *Handler) lifecycle(c *gin.Context, fn func(context.Context, int64) error, status int) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	if err := fn(c.Request.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			apiutil.Error(c, http.StatusNotFound, "source not found")
			return
		}
		logrus.WithError(err).Error("failed to process source lifecycle action")
		apiutil.Error(c, http.StatusInternalServerError, "failed to process source")
		return
	}

	c.Status(status)
}

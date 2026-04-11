package documents

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
	group := r.Group("/documents")
	group.GET("", h.List)
	group.GET("/:id", h.Get)
	group.POST("/:id/reindex", h.Reindex)
	group.POST("/:id/archive", h.Archive)
	group.POST("/:id/mark-deleted", h.MarkDeleted)
}

func (h *Handler) List(c *gin.Context) {
	var req ListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	items, err := h.service.List(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to list documents")
		apiutil.Error(c, http.StatusInternalServerError, "failed to list documents")
		return
	}

	c.JSON(http.StatusOK, ListResponse{Documents: items})
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
			apiutil.Error(c, http.StatusNotFound, "document not found")
			return
		}
		logrus.WithError(err).Error("failed to get document")
		apiutil.Error(c, http.StatusInternalServerError, "failed to get document")
		return
	}

	c.JSON(http.StatusOK, item)
}

func (h *Handler) Reindex(c *gin.Context) {
	h.lifecycle(c, h.service.Reindex, http.StatusAccepted)
}

func (h *Handler) Archive(c *gin.Context) {
	h.lifecycle(c, h.service.Archive, http.StatusAccepted)
}

func (h *Handler) MarkDeleted(c *gin.Context) {
	h.lifecycle(c, h.service.MarkDeleted, http.StatusAccepted)
}

func (h *Handler) lifecycle(c *gin.Context, fn func(context.Context, int64) error, status int) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	if err := fn(c.Request.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			apiutil.Error(c, http.StatusNotFound, "document not found")
			return
		}
		logrus.WithError(err).Error("failed to process document action")
		apiutil.Error(c, http.StatusInternalServerError, "failed to process document")
		return
	}

	c.Status(status)
}

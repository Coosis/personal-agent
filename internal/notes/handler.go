package notes

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
	group := r.Group("/notes")
	group.POST("", h.Create)
	group.GET("", h.List)
	group.GET("/:id", h.Get)
	group.PUT("/:id", h.Update)
	group.DELETE("/:id", h.Delete)
	group.POST("/:id/reindex", h.Reindex)
	group.POST("/:id/archive", h.Archive)
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	note, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to create note")
		apiutil.Error(c, http.StatusInternalServerError, "failed to create note")
		return
	}

	c.JSON(http.StatusCreated, note)
}

func (h *Handler) List(c *gin.Context) {
	var req ListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	items, err := h.service.List(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to list notes")
		apiutil.Error(c, http.StatusInternalServerError, "failed to list notes")
		return
	}

	c.JSON(http.StatusOK, ListResponse{Notes: items})
}

func (h *Handler) Get(c *gin.Context) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	note, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiutil.Error(c, http.StatusNotFound, "note not found")
			return
		}
		logrus.WithError(err).Error("failed to get note")
		apiutil.Error(c, http.StatusInternalServerError, "failed to get note")
		return
	}

	c.JSON(http.StatusOK, note)
}

func (h *Handler) Update(c *gin.Context) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	note, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiutil.Error(c, http.StatusNotFound, "note not found")
			return
		}
		logrus.WithError(err).Error("failed to update note")
		apiutil.Error(c, http.StatusInternalServerError, "failed to update note")
		return
	}

	c.JSON(http.StatusOK, note)
}

func (h *Handler) Delete(c *gin.Context) {
	h.lifecycle(c, h.service.Delete, http.StatusOK)
}

func (h *Handler) Reindex(c *gin.Context) {
	h.lifecycle(c, h.service.Reindex, http.StatusAccepted)
}

func (h *Handler) Archive(c *gin.Context) {
	h.lifecycle(c, h.service.Archive, http.StatusAccepted)
}

func (h *Handler) lifecycle(c *gin.Context, fn func(context.Context, int64) error, status int) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	if err := fn(c.Request.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			apiutil.Error(c, http.StatusNotFound, "note not found")
			return
		}
		logrus.WithError(err).Error("failed to process note lifecycle action")
		apiutil.Error(c, http.StatusInternalServerError, "failed to process note")
		return
	}

	c.Status(status)
}

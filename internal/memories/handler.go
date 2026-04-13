package memories

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
	group := r.Group("/memories")
	group.POST("", h.Create)
	group.GET("", h.List)
	group.GET("/:id", h.Get)
	group.PUT("/:id", h.Update)
	group.DELETE("/:id", h.Delete)
	group.POST("/:id/archive", h.Archive)
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	item, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to create memory")
		apiutil.Error(c, http.StatusInternalServerError, "failed to create memory")
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
		logrus.WithError(err).Error("failed to list memories")
		apiutil.Error(c, http.StatusInternalServerError, "failed to list memories")
		return
	}

	c.JSON(http.StatusOK, ListResponse{Memories: items})
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
			apiutil.Error(c, http.StatusNotFound, "memory not found")
			return
		}
		logrus.WithError(err).Error("failed to get memory")
		apiutil.Error(c, http.StatusInternalServerError, "failed to get memory")
		return
	}

	c.JSON(http.StatusOK, item)
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

	item, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apiutil.Error(c, http.StatusNotFound, "memory not found")
			return
		}
		logrus.WithError(err).Error("failed to update memory")
		apiutil.Error(c, http.StatusInternalServerError, "failed to update memory")
		return
	}

	c.JSON(http.StatusOK, item)
}

func (h *Handler) Delete(c *gin.Context) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			apiutil.Error(c, http.StatusNotFound, "memory not found")
			return
		}
		logrus.WithError(err).Error("failed to delete memory")
		apiutil.Error(c, http.StatusInternalServerError, "failed to delete memory")
		return
	}

	c.Status(http.StatusOK)
}

func (h *Handler) Archive(c *gin.Context) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.service.Archive(c.Request.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			apiutil.Error(c, http.StatusNotFound, "memory not found")
			return
		}
		logrus.WithError(err).Error("failed to archive memory")
		apiutil.Error(c, http.StatusInternalServerError, "failed to archive memory")
		return
	}

	c.Status(http.StatusOK)
}

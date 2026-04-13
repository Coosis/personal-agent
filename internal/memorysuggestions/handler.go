package memorysuggestions

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
	group := r.Group("/memory-suggestions")
	group.GET("", h.List)
	group.GET("/:id", h.Get)
	group.POST("/:id/accept", h.Accept)
	group.POST("/:id/reject", h.Reject)
}

func (h *Handler) List(c *gin.Context) {
	var req ListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	items, err := h.service.List(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to list memory suggestions")
		apiutil.Error(c, http.StatusInternalServerError, "failed to list memory suggestions")
		return
	}

	c.JSON(http.StatusOK, ListResponse{Suggestions: items})
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
			apiutil.Error(c, http.StatusNotFound, "memory suggestion not found")
			return
		}
		logrus.WithError(err).Error("failed to get memory suggestion")
		apiutil.Error(c, http.StatusInternalServerError, "failed to get memory suggestion")
		return
	}

	c.JSON(http.StatusOK, item)
}

func (h *Handler) Accept(c *gin.Context) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	resp, err := h.service.Accept(c.Request.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			apiutil.Error(c, http.StatusNotFound, "memory suggestion not found")
		case errors.Is(err, ErrInvalidState):
			apiutil.Error(c, http.StatusConflict, "memory suggestion is not pending")
		default:
			logrus.WithError(err).Error("failed to accept memory suggestion")
			apiutil.Error(c, http.StatusInternalServerError, "failed to accept memory suggestion")
		}
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) Reject(c *gin.Context) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	item, err := h.service.Reject(c.Request.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			apiutil.Error(c, http.StatusNotFound, "memory suggestion not found")
		case errors.Is(err, ErrInvalidState):
			apiutil.Error(c, http.StatusConflict, "memory suggestion is not pending")
		default:
			logrus.WithError(err).Error("failed to reject memory suggestion")
			apiutil.Error(c, http.StatusInternalServerError, "failed to reject memory suggestion")
		}
		return
	}

	c.JSON(http.StatusOK, item)
}

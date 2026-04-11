package uploads

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
	group := r.Group("/uploads")
	group.POST("", h.Create)
	group.GET("", h.List)
	group.GET("/:id", h.Get)
	group.DELETE("/:id", h.Delete)
	group.POST("/:id/reindex", h.Reindex)
	group.POST("/:id/archive", h.Archive)
}

func (h *Handler) Create(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "file is required")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "failed to open upload")
		return
	}
	defer file.Close()

	item, err := h.service.Create(c.Request.Context(), CreateRequest{
		DisplayName: c.PostForm("display_name"),
		Filename:    fileHeader.Filename,
		ContentType: fileHeader.Header.Get("Content-Type"),
	}, file)
	if err != nil {
		logrus.WithError(err).Error("failed to create upload")
		apiutil.Error(c, http.StatusInternalServerError, "failed to create upload")
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
		logrus.WithError(err).Error("failed to list uploads")
		apiutil.Error(c, http.StatusInternalServerError, "failed to list uploads")
		return
	}

	c.JSON(http.StatusOK, ListResponse{Uploads: items})
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
			apiutil.Error(c, http.StatusNotFound, "upload not found")
			return
		}
		logrus.WithError(err).Error("failed to get upload")
		apiutil.Error(c, http.StatusInternalServerError, "failed to get upload")
		return
	}

	c.JSON(http.StatusOK, item)
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
			apiutil.Error(c, http.StatusNotFound, "upload not found")
			return
		}
		logrus.WithError(err).Error("failed to process upload lifecycle action")
		apiutil.Error(c, http.StatusInternalServerError, "failed to process upload")
		return
	}

	c.Status(status)
}

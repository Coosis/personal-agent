package search

import (
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
	group := r.Group("/search")
	group.POST("", h.Search)
	group.POST("/debug", h.Debug)
}

func (h *Handler) Search(c *gin.Context) {
	var req SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	results, err := h.service.Search(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to search knowledge")
		apiutil.Error(c, http.StatusInternalServerError, "failed to search knowledge")
		return
	}

	c.JSON(http.StatusOK, SearchResponse{Results: results})
}

func (h *Handler) Debug(c *gin.Context) {
	var req SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.service.Debug(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to debug search")
		apiutil.Error(c, http.StatusInternalServerError, "failed to debug search")
		return
	}

	c.JSON(http.StatusOK, result)
}

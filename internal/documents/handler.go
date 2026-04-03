package documents

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// Handler handles document HTTP operations
type Handler struct {
	service *Service
}

// NewHandler creates a new document handler
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers document routes
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	docs := r.Group("/documents")
	docs.GET("", h.List)
	docs.GET("/:id", h.Get)
	docs.DELETE("/:id", h.Delete)
	docs.POST("/:id/reindex", h.Reindex)
	docs.POST("/scan", h.Scan)
}

// parseID helper to parse int64 id from URL param
func parseID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}

// List GET /api/v1/documents
func (h *Handler) List(c *gin.Context) {
	var req ListRequest
	err := c.ShouldBindQuery(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	docs, err := h.service.List(c.Request.Context(), req)
	if err != nil {
		logrus.WithError(err).Error("failed to list documents")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list documents"})
		return
	}

	c.JSON(http.StatusOK, ListResponse{Documents: docs})
}

// Get GET /api/v1/documents/:id
func (h *Handler) Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	doc, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
			return
		}
		logrus.WithError(err).Error("failed to get document")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get document"})
		return
	}

	c.JSON(http.StatusOK, doc)
}

// Delete DELETE /api/v1/documents/:id
func (h *Handler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	err = h.service.Delete(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
			return
		}
		logrus.WithError(err).Error("failed to delete document")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete document"})
		return
	}

	c.Status(http.StatusNoContent)
}

// Reindex POST /api/v1/documents/:id/reindex
func (h *Handler) Reindex(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	err = h.service.Reindex(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
			return
		}
		logrus.WithError(err).Error("failed to reindex document")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reindex document"})
		return
	}

	c.Status(http.StatusAccepted)
}

// Scan POST /api/v1/documents/scan
func (h *Handler) Scan(c *gin.Context) {
	err := h.service.Scan(c.Request.Context())
	if err != nil {
		logrus.WithError(err).Error("failed to trigger scan")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to trigger scan"})
		return
	}

	c.Status(http.StatusAccepted)
}

package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/Coosis/personal-agent/internal/agenthttp"
	"github.com/Coosis/personal-agent/internal/agentruns"
	"github.com/Coosis/personal-agent/internal/config"
	"github.com/Coosis/personal-agent/internal/conversations"
	"github.com/Coosis/personal-agent/internal/db"
	"github.com/Coosis/personal-agent/internal/documents"
	"github.com/Coosis/personal-agent/internal/jobs"
	"github.com/Coosis/personal-agent/internal/memories"
	"github.com/Coosis/personal-agent/internal/memorysuggestions"
	"github.com/Coosis/personal-agent/internal/notes"
	"github.com/Coosis/personal-agent/internal/search"
	"github.com/Coosis/personal-agent/internal/sources"
	"github.com/Coosis/personal-agent/internal/uploads"
)

var ErrShutdownTimeout = errors.New("server shutdown timeout")

type Server struct {
	httpServer *http.Server
	config     *config.Config
	db         *db.DB
}

func New(cfg *config.Config, database *db.DB) (*Server, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(loggingMiddleware())

	s := &Server{
		httpServer: &http.Server{
			Addr:        fmt.Sprintf("%s:%s", cfg.Host, cfg.Port),
			Handler:     router,
			ReadTimeout: 30 * time.Second,
			// Disable total write timeout so SSE responses can stream longer
			// than 30 seconds without the server killing the connection.
			WriteTimeout: 0,
		},
		config: cfg,
		db:     database,
	}

	s.registerRoutes(router)
	return s, nil
}

func (s *Server) registerRoutes(r *gin.Engine) {
	api := r.Group("/api/v1")
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	notes.NewHandler(notes.NewService(s.db)).RegisterRoutes(api)
	memories.NewHandler(memories.NewService(s.db)).RegisterRoutes(api)
	memorysuggestions.NewHandler(memorysuggestions.NewService(s.db)).RegisterRoutes(api)
	uploads.NewHandler(uploads.NewService(s.db, s.config.StorageRoot)).RegisterRoutes(api)
	sources.NewHandler(sources.NewService(s.db)).RegisterRoutes(api)
	documents.NewHandler(documents.NewService(s.db)).RegisterRoutes(api)
	search.NewHandler(search.NewService(s.db)).RegisterRoutes(api)
	jobs.NewHandler(jobs.NewService(s.db)).RegisterRoutes(api)
	agentruns.NewHandler(agentruns.NewService(s.db)).RegisterRoutes(api)

	convSvc := conversations.NewService(s.db, agenthttp.New(s.config.AgentURL))
	conversations.NewHandler(convSvc).RegisterRoutes(api)
}

func (s *Server) Start(context.Context) error {
	logrus.WithField("addr", s.httpServer.Addr).Info("starting server")
	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	logrus.Info("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("%w: %v", ErrShutdownTimeout, err)
	}
	return nil
}

func loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		if raw != "" {
			path = path + "?" + raw
		}

		entry := logrus.WithFields(logrus.Fields{
			"status":  c.Writer.Status(),
			"latency": latency,
			"ip":      c.ClientIP(),
			"method":  c.Request.Method,
			"path":    path,
		})

		switch {
		case len(c.Errors) > 0:
			entry.Error(c.Errors.String())
		case c.Writer.Status() >= 500:
			entry.Error()
		case c.Writer.Status() >= 400:
			entry.Warn()
		default:
			entry.Info()
		}
	}
}

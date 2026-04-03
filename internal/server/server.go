package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/Coosis/personal-agent/internal/config"
	"github.com/Coosis/personal-agent/internal/conversations"
	"github.com/Coosis/personal-agent/internal/db"
	"github.com/Coosis/personal-agent/internal/documents"
	"github.com/Coosis/personal-agent/internal/watcher"
	"github.com/Coosis/personal-agent/internal/worker"
)

// Errors
var ErrShutdownTimeout = errors.New("server shutdown timeout")

// Server holds HTTP server and dependencies
type Server struct {
	httpServer *http.Server
	config     *config.Config
	db         *db.DB
	watcherSvc *watcher.Service
	workerPool *worker.Pool
}

// New creates a new server, and tries to start the file watcher
func New(cfg *config.Config, database *db.DB) (*Server, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(loggingMiddleware())

	// Initialize watcher service
	watcherSvc, err := watcher.NewService(database)
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher service: %w", err)
	}

	// Initialize worker pool
	workerPool := worker.NewPool(database, cfg.WorkerPoolSize)

	s := &Server{
		httpServer: &http.Server{
			Addr:         fmt.Sprintf("%s:%s", cfg.Host, cfg.Port),
			Handler:      router,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		},
		config:     cfg,
		db:         database,
		watcherSvc: watcherSvc,
		workerPool: workerPool,
	}

	s.registerRoutes(router)

	// Starts watcher on server startup
	if err := s.watcherSvc.StartWatcher(context.Background()); err != nil {
		logrus.WithError(err).Warn("failed to start file watcher on server startup")
		return nil, err
	}

	return s, nil
}

// registerRoutes sets up all API routes
func (s *Server) registerRoutes(r *gin.Engine) {
	api := r.Group("/api/v1")

	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Documents domain
	docService := documents.NewService(s.db)
	docHandler := documents.NewHandler(docService)
	docHandler.RegisterRoutes(api)

	// Conversations domain
	convService := conversations.NewService(s.db)
	convHandler := conversations.NewHandler(convService)
	convHandler.RegisterRoutes(api)

	// Watcher domain
	watchHandler := watcher.NewHandler(s.watcherSvc)
	watchHandler.RegisterRoutes(api)
}

// Start runs the server, starts the worker pool and optionally starts the file watcher
func (s *Server) Start(ctx context.Context) error {
	// Start worker pool
	s.workerPool.Start(ctx)

	// Start file watcher if watch directories are configured
	dirs, err := s.db.Queries.ListWatchDirectories(ctx)
	if err != nil {
		logrus.WithError(err).Warn("failed to list watch directories")
	}

	if len(dirs) > 0 {
		if err := s.watcherSvc.StartWatcher(ctx); err != nil {
			logrus.WithError(err).Warn("failed to start file watcher")
		}
	}

	logrus.WithField("addr", s.httpServer.Addr).Info("starting server")
	err = s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown gracefully stops the server
func (s *Server) Shutdown(ctx context.Context) error {
	logrus.Info("shutting down server")

	// Stop worker pool first (finish current work)
	s.workerPool.Stop()

	// Stop file watcher
	if err := s.watcherSvc.StopWatcher(); err != nil {
		logrus.WithError(err).Warn("error stopping file watcher")
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := s.httpServer.Shutdown(shutdownCtx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrShutdownTimeout, err)
	}
	return nil
}

// loggingMiddleware logs requests with logrus
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

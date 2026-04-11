package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"

	"github.com/Coosis/personal-agent/internal/config"
	"github.com/Coosis/personal-agent/internal/db"
	"github.com/Coosis/personal-agent/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		logrus.WithError(err).Fatal("failed to load config")
	}

	config.SetupLogging(cfg.LogLevel)

	if err := os.MkdirAll(cfg.StorageRoot, 0o755); err != nil {
		logrus.WithError(err).Fatal("failed to create storage root")
	}

	database, err := db.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logrus.WithError(err).Fatal("failed to connect database")
	}
	defer database.Close()

	srv, err := server.New(cfg, database)
	if err != nil {
		logrus.WithError(err).Fatal("failed to create server")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		err := srv.Start(ctx)
		if err != nil {
			logrus.WithError(err).Fatal("server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logrus.Info("shutting down")
	err = srv.Shutdown(context.Background())
	if err != nil {
		logrus.WithError(err).Error("shutdown error")
	}
}

package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	sqlc "github.com/Coosis/personal-agent/sqlc"
)

// Errors
var (
	ErrConnectionFailed = errors.New("database connection failed")
	ErrPingFailed       = errors.New("database ping failed")
)

// DB wraps the sqlc queries with connection pool
type DB struct {
	Pool    *pgxpool.Pool
	Queries *sqlc.Queries
}

// New creates a new database connection and sqlc queries
func New(ctx context.Context, connString string) (*DB, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, err
	}

	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnIdleTime = 30 * time.Minute
	config.MaxConnLifetime = 2 * time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, ErrConnectionFailed
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, ErrPingFailed
	}

	queries := sqlc.New(pool)

	logrus.Info("database connected")
	return &DB{Pool: pool, Queries: queries}, nil
}

// Close closes the database connection pool
func (d *DB) Close() {
	if d.Pool != nil {
		d.Pool.Close()
		logrus.Info("database disconnected")
	}
}

// WithTx executes the given function within a database transaction
func (d *DB) WithTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	q := d.Queries.WithTx(tx)
	if err := fn(q); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

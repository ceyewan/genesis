package db

import "time"

// Config captures essential database settings used by the default provider.
type Config struct {
	Driver          string        `json:"driver" yaml:"driver"`
	DSN             string        `json:"dsn" yaml:"dsn"`
	MaxOpenConns    int           `json:"max_open_conns" yaml:"max_open_conns"`
	MaxIdleConns    int           `json:"max_idle_conns" yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime" yaml:"conn_max_lifetime"`
	SlowQuery       time.Duration `json:"slow_query" yaml:"slow_query"`
}

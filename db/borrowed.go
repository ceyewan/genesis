package db

import (
	"maps"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"github.com/ceyewan/genesis/xerrors"
)

// openBorrowed owns the GORM handle, callbacks and plugins, never the SQL pool.
// Connector plugins/callbacks are deliberately not inherited.
func openBorrowed(base *gorm.DB, log gormlogger.Interface) (*gorm.DB, error) {
	pool, err := base.DB()
	if err != nil {
		return nil, xerrors.Wrap(err, "get borrowed sql pool")
	}
	var dialector gorm.Dialector
	switch d := base.Dialector.(type) {
	case *mysql.Dialector:
		cfg := *d.Config
		cfg.Conn = pool
		cfg.SkipInitializeWithVersion = true
		dialector = mysql.New(cfg)
	case *postgres.Dialector:
		cfg := *d.Config
		cfg.Conn = pool
		dialector = postgres.New(cfg)
	case *sqlite.Dialector:
		copy := *d
		copy.Conn = pool
		dialector = &copy
	default:
		return nil, xerrors.Wrap(ErrInvalidConfig, "unsupported borrowed GORM dialector")
	}

	// Build a fresh config: copying gorm.Config also copies its private schema
	// cache and plugin state. Only supported behavioral settings are inherited.
	naming := base.NamingStrategy
	if strategy, ok := naming.(*schema.NamingStrategy); ok && strategy != nil {
		copy := *strategy
		naming = &copy
	}
	cfg := &gorm.Config{
		SkipDefaultTransaction:                   base.SkipDefaultTransaction,
		NamingStrategy:                           naming,
		FullSaveAssociations:                     base.FullSaveAssociations,
		Logger:                                   log,
		NowFunc:                                  base.NowFunc,
		DryRun:                                   base.DryRun,
		PrepareStmt:                              base.PrepareStmt,
		PrepareStmtMaxSize:                       base.PrepareStmtMaxSize,
		PrepareStmtTTL:                           base.PrepareStmtTTL,
		DisableAutomaticPing:                     true,
		DisableForeignKeyConstraintWhenMigrating: base.DisableForeignKeyConstraintWhenMigrating,
		IgnoreRelationshipsWhenMigrating:         base.IgnoreRelationshipsWhenMigrating,
		DisableNestedTransaction:                 base.DisableNestedTransaction,
		AllowGlobalUpdate:                        base.AllowGlobalUpdate,
		QueryFields:                              base.QueryFields,
		CreateBatchSize:                          base.CreateBatchSize,
		TranslateError:                           base.TranslateError,
		PropagateUnscoped:                        base.PropagateUnscoped,
		ClauseBuilders:                           maps.Clone(base.ClauseBuilders),
	}
	handle, err := gorm.Open(borrowedDialector{dialector}, cfg)
	if err != nil {
		return nil, err
	}
	// Preserve dialect-discovered clause lists (e.g. MariaDB RETURNING) without
	// copying callbacks or their captured state from the Connector.
	handle.Callback().Create().Clauses = append([]string(nil), base.Callback().Create().Clauses...)
	handle.Callback().Query().Clauses = append([]string(nil), base.Callback().Query().Clauses...)
	handle.Callback().Update().Clauses = append([]string(nil), base.Callback().Update().Clauses...)
	handle.Callback().Delete().Clauses = append([]string(nil), base.Callback().Delete().Clauses...)
	handle.Callback().Row().Clauses = append([]string(nil), base.Callback().Row().Clauses...)
	handle.Callback().Raw().Clauses = append([]string(nil), base.Callback().Raw().Clauses...)
	return handle, nil
}

// GORM closes db.DB() when Initialize fails. Detach the borrowed pool on that
// failure path before GORM's cleanup runs; the Connector retains ownership.
type borrowedDialector struct{ gorm.Dialector }

func (d borrowedDialector) Initialize(db *gorm.DB) error {
	if err := d.Dialector.Initialize(db); err != nil {
		db.ConnPool = nil
		if db.Statement != nil {
			db.Statement.ConnPool = nil
		}
		return err
	}
	// Preserve concrete dialect capabilities such as ErrorTranslator.
	db.Dialector = d.Dialector
	return nil
}

func (d borrowedDialector) Apply(cfg *gorm.Config) error {
	if apply, ok := d.Dialector.(interface{ Apply(*gorm.Config) error }); ok {
		now := cfg.NowFunc
		err := apply.Apply(cfg)
		if now != nil {
			cfg.NowFunc = now
		}
		return err
	}
	return nil
}

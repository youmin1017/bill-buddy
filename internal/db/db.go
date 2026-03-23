// Package db
package db

import (
	"context"
	"database/sql"
	"fmt"

	"bill-buddy/ent"
	"bill-buddy/internal/config"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/samber/do/v2"
	_ "modernc.org/sqlite"
)

// NewClient creates an ent client backed by SQLite.
func NewClient(i do.Injector) (*ent.Client, error) {
	cfg := do.MustInvoke[*config.Config](i)

	sqlDB, err := sql.Open("sqlite", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed opening sqlite: %w", err)
	}

	drv := entsql.OpenDB(dialect.SQLite, sqlDB)
	client := ent.NewClient(ent.Driver(drv))

	if err := client.Schema.Create(context.Background()); err != nil {
		return nil, fmt.Errorf("failed creating schema resources: %w", err)
	}

	return client, nil
}

var Package = do.Package(
	do.Lazy(NewClient),
)

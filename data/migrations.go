package data

import (
	"embed"
	"fmt"

	"github.com/standards-lab/sqlate/migrate"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// AppSet names Mikoshi's migration set, recorded in migrate.DefaultTable.
const AppSet = "app"

// Migrations is Mikoshi's migration set, NNNN_name.{up,down}.sql under
// migrations/. Each embedding space is registered by a migration of its
// own: its row at a fixed ID and its partial HNSW index. A layout defect is
// a wiring defect and panics at cold start.
func Migrations() migrate.Set {
	ms, err := migrate.Files(migrationFiles, "migrations")
	if err != nil {
		panic(fmt.Sprintf("data: %v", err))
	}
	return migrate.Set{Name: AppSet, Migrations: ms}
}

// Package statements owns every complete SQLite statement and schema
// definition used by the core program, per the SQL standard.
package statements

const (
	SchemaVersion       = 1
	MetaApplicationID   = 0x44534d4d // DSMM
	MemoryApplicationID = 0x44534d57 // DSMW

	ReadApplicationID = `PRAGMA application_id`
	ReadSchemaVersion = `PRAGMA user_version`
	BeginImmediate    = `BEGIN IMMEDIATE`
	Commit            = `COMMIT`
	Rollback          = `ROLLBACK`
)

var Setup = []string{
	`PRAGMA foreign_keys = ON`,
	`PRAGMA busy_timeout = 1000`,
	`PRAGMA journal_mode = WAL`,
}

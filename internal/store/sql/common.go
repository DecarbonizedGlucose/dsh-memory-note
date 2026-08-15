// Package sql owns all SQLite statements used by the application.
package sql

var Setup = []string{
	`PRAGMA foreign_keys = ON`,
	`PRAGMA busy_timeout = 2000`,
	`PRAGMA journal_mode = WAL`,
}

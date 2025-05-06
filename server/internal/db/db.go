package db

import (
	"captcha-solver/internal/config"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"log"
)

var err error

func DB_Connect() {
	config.DB, err = sql.Open("sqlite3", "./app.db")
	if err != nil {
		log.Fatalf("Error opening DB: %v", err)
	}
	if err := createTables(); err != nil {
		log.Fatalf("Error creating tables: %v", err)
	}
	config.CreateDefaultAdmin()
}

func createTables() error {
	// Create users table.
	_, err := config.DB.Exec(`
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL,
		api_key TEXT,
		balance REAL NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL
	)
	`)
	if err != nil {
		return err
	}

	// Create tasks table.
	_, err = config.DB.Exec(`
	CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		solver_id INTEGER,
		captcha_type TEXT NOT NULL,
		sitekey TEXT NOT NULL,
		target_url TEXT NOT NULL,
		captcha_response TEXT,
		created_at DATETIME NOT NULL,
		FOREIGN KEY(user_id) REFERENCES users(id),
		FOREIGN KEY(solver_id) REFERENCES users(id)
	)
	`)
	if err != nil {
		return err
	}

	// Check if the tasks table has the created_at column.
	rows, err := config.DB.Query("PRAGMA table_info(tasks)")
	if err != nil {
		return err
	}
	defer rows.Close()

	hasCreatedAt := false
	for rows.Next() {
		var cid int
		var name string
		var ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		if name == "created_at" {
			hasCreatedAt = true
			break
		}
	}

	// If created_at column does not exist, alter the table.
	if !hasCreatedAt {
		_, err = config.DB.Exec("ALTER TABLE tasks ADD COLUMN created_at DATETIME NOT NULL DEFAULT (datetime('now'))")
		if err != nil {
			return err
		}
	}

	return nil
}

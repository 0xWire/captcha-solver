package db

import (
	"captcha-solver/internal/config"
	"database/sql"
	"log"

	_ "github.com/mattn/go-sqlite3"
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
		api_key TEXT UNIQUE,
		balance REAL NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
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
		status TEXT NOT NULL DEFAULT 'pending',
		error_message TEXT,
		attempts INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
		solved_at DATETIME,
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
		FOREIGN KEY(solver_id) REFERENCES users(id) ON DELETE SET NULL
	)
	`)
	if err != nil {
		return err
	}

	// Create indexes for tasks table
	_, err = config.DB.Exec(`
	CREATE INDEX IF NOT EXISTS idx_tasks_user_id ON tasks(user_id);
	CREATE INDEX IF NOT EXISTS idx_tasks_solver_id ON tasks(solver_id);
	CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);
	CREATE INDEX IF NOT EXISTS idx_tasks_updated_at ON tasks(updated_at);
	CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
	CREATE INDEX IF NOT EXISTS idx_tasks_captcha_type ON tasks(captcha_type);
	CREATE INDEX IF NOT EXISTS idx_tasks_pending ON tasks(status) WHERE status = 'pending';
	`)
	if err != nil {
		return err
	}

	// Create triggers for updated_at
	_, err = config.DB.Exec(`
	CREATE TRIGGER IF NOT EXISTS update_users_timestamp 
	AFTER UPDATE ON users
	BEGIN
		UPDATE users SET updated_at = datetime('now') WHERE id = NEW.id;
	END;

	CREATE TRIGGER IF NOT EXISTS update_tasks_timestamp 
	AFTER UPDATE ON tasks
	BEGIN
		UPDATE tasks SET updated_at = datetime('now') WHERE id = NEW.id;
	END;
	`)
	if err != nil {
		return err
	}

	return nil
}

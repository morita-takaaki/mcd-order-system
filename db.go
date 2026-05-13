package main

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

func InitDB() *sql.DB {
	os.MkdirAll("logs", 0755)

	db, err := sql.Open("sqlite3", "order.db")
	if err != nil {
		log.Fatal(err)
	}

	sqlStmt := `
CREATE TABLE IF NOT EXISTS order_items (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	order_no TEXT NOT NULL,
	terminal_no TEXT NOT NULL,
	order_status TEXT NOT NULL,
	item_no INTEGER NOT NULL,
	menu_name TEXT NOT NULL,
	unit_price INTEGER NOT NULL,
	quantity INTEGER NOT NULL,
	subtotal INTEGER NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`

	_, err = db.Exec(sqlStmt)
	if err != nil {
		log.Fatal(err)
	}

	return db
}

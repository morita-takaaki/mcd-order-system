package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func InitDB() {
	dbPath := "order.db"
	var err error

	// 同時書き込み対策として busy_timeout=5000 を設定
	dsn := fmt.Sprintf("%s?_busy_timeout=5000", dbPath)
	db, err = sql.Open("sqlite3", dsn)
	if err != nil {
		Logger.Fatalf("Failed to open database: %v", err)
	}

	// 同時書き込み競合を完全に回避するため、最大接続数を1に制限
	db.SetMaxOpenConns(1)

	schema := `
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

	if _, err := db.Exec(schema); err != nil {
		Logger.Fatalf("Failed to create tables: %v", err)
	}
}

func CloseDB() {
	if db != nil {
		db.Close()
	}
}

// 採番処理と注文データ登録を同一トランザクション内でカプセル化（重複防止）
func createOrderInTx(terminalNo string, items []OrderItemInput) (string, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	today := time.Now().Format("0102") // MMDD形式
	prefix := today + "-"

	// 本日の最終オーダー番号を取得
	var lastOrderNo string
	err = tx.QueryRow(`
		SELECT order_no FROM order_items 
		WHERE order_no LIKE ?1 
		ORDER BY id DESC LIMIT 1`, prefix+"%").Scan(&lastOrderNo)

	nextSeq := 1
	if err == nil && len(lastOrderNo) == 8 {
		var seq int
		_, errParse := fmt.Sscanf(lastOrderNo[5:], "%d", &seq)
		if errParse == nil {
			nextSeq = seq + 1
		}
	}

	orderNo := fmt.Sprintf("%s%03d", prefix, nextSeq)
	status := "オーダ受信済み"

	for i, item := range items {
		subtotal := item.UnitPrice * item.Quantity
		_, err := tx.Exec(`
			INSERT INTO order_items (order_no, terminal_no, order_status, item_no, menu_name, unit_price, quantity, subtotal)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			orderNo, terminalNo, status, i+1, item.MenuName, item.UnitPrice, item.Quantity, subtotal)
		if err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	Logger.Printf("[DB登録内容] OrderNo: %s, Items Total: %d レコード挿入完了", orderNo, len(items))
	return orderNo, nil
}
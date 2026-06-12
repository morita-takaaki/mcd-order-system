package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func initDB() {
	dbPath := "order.db"
	
	// SQLite接続文字列の設定
	dsn := fmt.Sprintf("%s?_busy_timeout=5000", dbPath)
	var err error
	db, err = sql.Open("sqlite3", dsn)
	if err != nil {
		log.Fatalf("DB接続失敗: %v", err)
	}

	// 同時書き込み対策の設定
	db.SetMaxOpenConns(1)

	// テーブル作成
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
		log.Fatalf("テーブル作成失敗: %v", err)
	}
}

// 注文番号の生成（同日内のユニーク番号 YYMMDDNNNN）
func generateOrderNo(tx *sql.Tx) (string, error) {
	today := time.Now().Format("060102")
	var count int
	// 本日登録された一意な注文番号の数をカウント
	query := "SELECT COUNT(DISTINCT order_no) FROM order_items WHERE order_no LIKE ?"
	err := tx.QueryRow(query, today+"%").Scan(&count)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%04d", today, count+1), nil
}

// 注文の保存
func insertOrder(req OrderRequest) (string, []LogOrderItem, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()

	orderNo, err := generateOrderNo(tx)
	if err != nil {
		return "", nil, err
	}

	var loggedItems []LogOrderItem

	for i, item := range req.Items {
		itemNo := i + 1
		query := `
		INSERT INTO order_items (order_no, terminal_no, order_status, item_no, menu_name, unit_price, quantity, subtotal)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
		
		_, err := tx.Exec(query, orderNo, req.TerminalNo, "オーダ受信済み", itemNo, item.MenuName, item.UnitPrice, item.Quantity, item.Subtotal)
		if err != nil {
			return "", nil, err
		}

		loggedItems = append(loggedItems, LogOrderItem{
			OrderNo:     orderNo,
			TerminalNo:  req.TerminalNo,
			OrderStatus: "オーダ受信済み",
			ItemNo:      itemNo,
			MenuName:    item.MenuName,
			UnitPrice:   item.UnitPrice,
			Quantity:    item.Quantity,
			Subtotal:    item.Subtotal,
		})
	}

	if err := tx.Commit(); err != nil {
		return "", nil, err
	}

	return orderNo, loggedItems, nil
}

// 厨房用注文一覧（オーダ受信済み）の取得
func getKitchenOrders() ([]KitchenOrder, error) {
	query := `
	SELECT order_no, menu_name, quantity 
	FROM order_items 
	WHERE order_status = 'オーダ受信済み'
	ORDER BY id ASC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orderMap := make(map[string][]KitchenOrderItem)
	var orderKeys []string // 順序維持用

	for rows.Next() {
		var oNo, mName string
		var qty int
		if err := rows.Scan(&oNo, &mName, &qty); err != nil {
			return nil, err
		}

		item := KitchenOrderItem{MenuName: mName, Quantity: qty}
		if _, exists := orderMap[oNo]; !exists {
			orderKeys = append(orderKeys, oNo)
		}
		orderMap[oNo] = append(orderMap[oNo], item)
	}

	orders := []KitchenOrder{}
	for _, k := range orderKeys {
		orders = append(orders, KitchenOrder{
			OrderNo: k,
			Items:   orderMap[k],
		})
	}

	return orders, nil
}

// 厨房注文ステータス更新（オーダ受信済み -> 調理済み）
func updateKitchenStatus(orderNo string) (int64, error) {
	query := "UPDATE order_items SET order_status = '調理済み' WHERE order_no = ? AND order_status = 'オーダ受信済み'"
	res, err := db.Exec(query, orderNo)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// 掲示板用情報（調理中・受け渡し可能）の取得
func getBoardOrders() ([]string, []string, error) {
	query := "SELECT DISTINCT order_no, order_status FROM order_items WHERE order_status IN ('オーダ受信済み', '調理済み') ORDER BY id ASC"
	rows, err := db.Query(query)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var cookingOrders []string
	var readyOrders []string

	for rows.Next() {
		var oNo, status string
		if err := rows.Scan(&oNo, &status); err != nil {
			return nil, nil, err
		}
		if status == "オーダ受信済み" {
			cookingOrders = append(cookingOrders, oNo)
		} else if status == "調理済み" {
			readyOrders = append(readyOrders, oNo)
		}
	}

	return cookingOrders, readyOrders, nil
}

// 掲示板注文ステータス更新（調理済み -> 受け渡し済み）
func updateBoardStatus(orderNo string) (int64, error) {
	query := "UPDATE order_items SET order_status = '受け渡し済み' WHERE order_no = ? AND order_status = '調理済み'"
	res, err := db.Exec(query, orderNo)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
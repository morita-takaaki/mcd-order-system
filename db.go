package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

// DB内部ステータス定義
const (
	StatusReceived  = "オーダ受信済み"
	StatusCooked    = "調理済み"
	StatusDelivered = "受け渡し済み"
)

// アプリケーション起動時のDB初期化処理
func initDB() {
	dbPath := "order.db"
	// 同時書き込み対策の設定を付与して接続
	dsn := fmt.Sprintf("%s?_busy_timeout=5000", dbPath)
	
	var err error
	db, err = sql.Open("sqlite3", dsn)
	if err != nil {
		logger.Fatalf("[ERROR] データベース接続失敗: %v", err)
	}

	// ライトロック対策として最大オープン接続数を1に制限
	db.SetMaxOpenConns(1)

	// テーブルの自動作成
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
		logger.Fatalf("[ERROR] テーブル作成失敗: %v", err)
	}
	logger.Println("[INFO] データベースが正常に初期化されました。")
}

// アプリケーション終了時のDBクローズ処理
func closeDB() {
	if db != nil {
		db.Close()
		logger.Println("[INFO] データベース接続を閉じました。")
	}
}

// OrderItem 型の定義（明細単位）
type OrderItem struct {
	ID         int       `json:"id,omitempty"`
	OrderNo    string    `json:"orderNo"`
	TerminalNo string    `json:"terminalNo,omitempty"`
	Status     string    `json:"orderStatus,omitempty"`
	ItemNo     int       `json:"itemNo,omitempty"`
	MenuName   string    `json:"menuName"`
	UnitPrice  int       `json:"unitPrice,omitempty"`
	Quantity   int       `json:"quantity"`
	Subtotal   int       `json:"subtotal,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
}

// OrderSummary 型の定義（注文単位の集約用）
type OrderSummary struct {
	OrderNo     string      `json:"orderNo"`
	TerminalNo  string      `json:"terminalNo"`
	OrderStatus string      `json:"orderStatus"`
	TotalAmount int         `json:"totalAmount"`
	Items       []OrderItem `json:"items"`
}

// 同一トランザクション内での採番および一括INSERT処理
func insertOrderWithSequence(terminalNo string, items []OrderItem) (string, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	now := time.Now()
	todayStr := now.Format("0102") // MMDD 形式

	// 本日の最新の連番を取得（日付が変わったら自動的に001スタートにするための処理）
	var maxOrderNo sql.NullString
	querySeq := `SELECT max(order_no) FROM order_items WHERE order_no LIKE ?`
	err = tx.QueryRow(querySeq, todayStr+"-%").Scan(&maxOrderNo)
	if err != nil {
		return "", err
	}

	nextSeq := 1
	if maxOrderNo.Valid && maxOrderNo.String != "" {
		// MMDD-NNN の下3桁から数値をパース
		var currentSeq int
		_, err := fmt.Sscanf(maxOrderNo.String, todayStr+"-%03d", &currentSeq)
		if err == nil {
			nextSeq = currentSeq + 1
		}
	}

	orderNo := fmt.Sprintf("%s-%03d", todayStr, nextSeq)
	logger.Printf("[DB_TX] 採番完了: %s (端末: %s)", orderNo, terminalNo)

	// 明細データのINSERT
	stmt, err := tx.Prepare(`
		INSERT INTO order_items (order_no, terminal_no, order_status, item_no, menu_name, unit_price, quantity, subtotal)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return "", err
	}
	defer stmt.Close()

	for i, item := range items {
		subtotal := item.UnitPrice * item.Quantity
		_, err = stmt.Exec(orderNo, terminalNo, StatusReceived, i+1, item.MenuName, item.UnitPrice, item.Quantity, subtotal)
		if err != nil {
			return "", err
		}
		logger.Printf("[DB_INSERT] 明細追加 - OrderNo: %s, ItemNo: %d, Menu: %s, Subtotal: %d", orderNo, i+1, item.MenuName, subtotal)
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return orderNo, nil
}

// 全注文またはステータス指定の注文をマップ・スライスで集約して取得
func getOrdersWithFilter(statusFilter string) ([]OrderSummary, error) {
	var rows *sql.Rows
	var err error

	baseQuery := `SELECT order_no, terminal_no, order_status, menu_name, quantity, unit_price, subtotal FROM order_items`
	if statusFilter != "" {
		rows, err = db.Query(baseQuery+" WHERE order_status = ? ORDER BY order_no ASC, item_no ASC", statusFilter)
	} else {
		rows, err = db.Query(baseQuery + " ORDER BY order_no ASC, item_no ASC")
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 順序を維持しつつ集約するための仕組み
	var list []OrderSummary
	orderMap := make(map[string]*OrderSummary)
	var orderNos []string

	for rows.Next() {
		var oNo, tNo, oStat, mName string
		var qty, uPrice, sub int
		if err := rows.Scan(&oNo, &tNo, &oStat, &mName, &qty, &uPrice, &sub); err != nil {
			return nil, err
		}

		if _, exists := orderMap[oNo]; !exists {
			orderNos = append(orderNos, oNo)
			orderMap[oNo] = &OrderSummary{
				OrderNo:     oNo,
				TerminalNo:  tNo,
				OrderStatus: oStat,
				TotalAmount: 0,
				Items:       []OrderItem{},
			}
		}

		orderMap[oNo].TotalAmount += sub
		orderMap[oNo].Items = append(orderMap[oNo].Items, OrderItem{
			MenuName:  mName,
			Quantity:  qty,
			UnitPrice: uPrice,
			Subtotal:  sub,
		})
	}

	for _, oNo := range orderNos {
		list = append(list, *orderMap[oNo])
	}

	return list, nil
}

// 掲示板用：ステータス別に注文番号の文字列スライスを抽出
func getOrderNosByStatus(status string) ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT order_no FROM order_items WHERE order_status = ? ORDER BY order_no ASC`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orderNos []string
	for rows.Next() {
		var orderNo string
		if err := rows.Scan(&orderNo); err != nil {
			return nil, err
		}
		orderNos = append(orderNos, orderNo)
	}
	return orderNos, nil
}

// ステータスの一括更新処理
func updateStatus(orderNo, newStatus string) (int64, error) {
	res, err := db.Exec(`UPDATE order_items SET order_status = ? WHERE order_no = ?`, newStatus, orderNo)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected > 0 {
		logger.Printf("[DB_UPDATE] OrderNo: %s をステータス: 「%s」に更新しました (影響行数: %d)", orderNo, newStatus, affected)
	}
	return affected, nil
}
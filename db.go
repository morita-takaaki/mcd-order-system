package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	// CGOを利用したSQLite3ドライバのインポート
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

// データベースの初期化処理
func initDB() {
	dbPath := "order.db"
	// 同時書き込み対策として _busy_timeout=5000 (5秒) を付与
	dsn := fmt.Sprintf("%s?_busy_timeout=5000", dbPath)

	var err error
	db, err = sql.Open("sqlite3", dsn)
	if err != nil {
		log.Fatalf("[FATAL] データベースのオープンに失敗しました: %v", err)
	}

	// 同時書き込み対策として、最大オープン接続数を1に厳格制限
	db.SetMaxOpenConns(1)

	// 接続テスト
	if err := db.Ping(); err != nil {
		log.Fatalf("[FATAL] データベースへのピン接続に失敗しました: %v", err)
	}

	log.Println("[INFO] データベースへの接続に成功しました。")

	// テーブルの自動生成
	createTableQuery := `
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

	if _, err := db.Exec(createTableQuery); err != nil {
		log.Fatalf("[FATAL] テーブルの作成に失敗しました: %v", err)
	}
	log.Println("[INFO] order_items テーブルを確認/作成しました。")
}

// データベースを閉じる処理
func closeDB() {
	if db != nil {
		if err := db.Close(); err != nil {
			log.Printf("[ERROR] データベースのクローズ中にエラーが発生しました: %v", err)
		} else {
			log.Println("[INFO] データベース接続を閉じました。")
		}
	}
}

// OrderItem のDBマッピング用構造体
type DBOrderItem struct {
	ID          int
	OrderNo     string
	TerminalNo  string
	OrderStatus string
	ItemNo      int
	MenuName    string
	UnitPrice   int
	Quantity    int
	Subtotal    int
	CreatedAt   time.Time
}

// トランザクション内での採番および注文情報の一括登録処理
func insertOrderWithSequence(terminalNo string, items []OrderItemInput) (string, error) {
	// 同一トランザクションの開始
	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("トランザクションの開始に失敗: %w", err)
	}
	// エラー発生時はロールバック
	defer tx.Rollback()

	// 現在の「月日」を取得（MMDD形式）
	now := time.Now()
	dateStr := now.Format("0102") // 0523 などの形式

	// 同日内の最新の order_no を取得して連番を決定する
	// order_no が 'MMDD-%' に前方一致するもののうち最大値を検索
	var maxOrderNo sql.NullString
	querySeq := `SELECT MAX(order_no) FROM order_items WHERE order_no LIKE ?`
	err = tx.QueryRow(querySeq, dateStr+"-%").Scan(&maxOrderNo)
	if err != nil {
		return "", fmt.Errorf("採番用のデータ取得に失敗: %w", err)
	}

	nextSeq := 1
	if maxOrderNo.Valid && maxOrderNo.String != "" {
		// MMDD-NNN の NNN 部分をパースする
		var currentSeq int
		_, err := fmt.Sscanf(maxOrderNo.String, dateStr+"-%d", &currentSeq)
		if err == nil {
			nextSeq = currentSeq + 1
		}
	}

	// 新しい注文番号の確定（例: 0523-001）
	newOrderNo := fmt.Sprintf("%s-%03d", dateStr, nextSeq)
	initialStatus := "オーダ受信済み"

	// 明細データのインサート処理
	insertQuery := `
	INSERT INTO order_items (
		order_no, terminal_no, order_status, item_no, menu_name, unit_price, quantity, subtotal
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?);`

	for idx, item := range items {
		itemNo := idx + 1
		subtotal := item.UnitPrice * item.Quantity

		_, err := tx.Exec(insertQuery, newOrderNo, terminalNo, initialStatus, itemNo, item.MenuName, item.UnitPrice, item.Quantity, subtotal)
		if err != nil {
			return "", fmt.Errorf("明細(行番号:%d)の登録に失敗: %w", err)
		}
	}

	// トランザクションのコミット
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("トランザクションのコミットに失敗: %w", err)
	}

	// ログへDB登録内容を出力
	log.Printf("[DB_INSERT] 注文番号: %s を登録しました。明細数: %d 件\n", newOrderNo, len(items))

	return newOrderNo, nil
}

// 全注文明細の取得（ステータスによるフィルタ対応）
func fetchAllOrderItems(statusFilter string) ([]DBOrderItem, error) {
	var query string
	var args []interface{}

	if statusFilter != "" {
		query = `SELECT id, order_no, terminal_no, order_status, item_no, menu_name, unit_price, quantity, subtotal, created_at 
		         FROM order_items WHERE order_status = ? ORDER BY order_no ASC, item_no ASC`
		args = append(args, statusFilter)
	} else {
		query = `SELECT id, order_no, terminal_no, order_status, item_no, menu_name, unit_price, quantity, subtotal, created_at 
		         FROM order_items ORDER BY order_no ASC, item_no ASC`
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []DBOrderItem
	for rows.Next() {
		var item DBOrderItem
		err := rows.Scan(&item.ID, &item.OrderNo, &item.TerminalNo, &item.OrderStatus, &item.ItemNo, &item.MenuName, &item.UnitPrice, &item.Quantity, &item.Subtotal, &item.CreatedAt)
		if err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, nil
}

// 特定の注文番号に紐づく明細一覧の取得
func fetchOrderItemsByNo(orderNo string) ([]DBOrderItem, error) {
	query := `SELECT id, order_no, terminal_no, order_status, item_no, menu_name, unit_price, quantity, subtotal, created_at 
	          FROM order_items WHERE order_no = ? ORDER BY item_no ASC`

	rows, err := db.Query(query, orderNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []DBOrderItem
	for rows.Next() {
		var item DBOrderItem
		err := rows.Scan(&item.ID, &item.OrderNo, &item.TerminalNo, &item.OrderStatus, &item.ItemNo, &item.MenuName, &item.UnitPrice, &item.Quantity, &item.Subtotal, &item.CreatedAt)
		if err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, nil
}

// 注文ステータスの更新処理
func updateOrderStatus(orderNo string, newStatus string) (int64, error) {
	query := `UPDATE order_items SET order_status = ? WHERE order_no = ?`
	res, err := db.Exec(query, newStatus, orderNo)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}

	if rowsAffected > 0 {
		log.Printf("[DB_UPDATE] 注文番号: %s のステータスを 「%s」 に更新しました。(影響行数: %d)\n", orderNo, newStatus, rowsAffected)
	}
	return rowsAffected, nil
}
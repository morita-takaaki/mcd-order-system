package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type OrderRequest struct {
	TerminalNo  string      `json:"terminalNo"`
	MessageType string      `json:"messageType"`
	TotalAmount int         `json:"totalAmount"`
	Items       []OrderItem `json:"items"`
}

type OrderItem struct {
	MenuName  string `json:"menuName"`
	UnitPrice int    `json:"unitPrice"`
	Quantity  int    `json:"quantity"`
}

type StatusRequest struct {
	OrderStatus string `json:"orderStatus"`
}

func OrdersHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		if r.Method == http.MethodPost {
			createOrder(w, r, db)
			return
		}

		if r.Method == http.MethodGet {
			getOrders(w, r, db)
			return
		}

		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func OrderDetailHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		path := strings.TrimPrefix(r.URL.Path, "/api/orders/")
		parts := strings.Split(path, "/")
		orderNo := parts[0]

		if r.Method == http.MethodGet && len(parts) == 1 {
			getOrderDetail(w, db, orderNo)
			return
		}

		if r.Method == http.MethodPut && len(parts) == 2 && parts[1] == "status" {
			updateStatus(w, r, db, orderNo)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}
}

func createOrder(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var req OrderRequest
	json.NewDecoder(r.Body).Decode(&req)

	if msg := validate(req); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	orderNo := createOrderNo(db)
	status := "オーダー受信"

	for i, item := range req.Items {
		subtotal := item.UnitPrice * item.Quantity
		db.Exec(`
INSERT INTO order_items
(order_no, terminal_no, order_status, item_no, menu_name, unit_price, quantity, subtotal)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			orderNo, req.TerminalNo, status, i+1,
			item.MenuName, item.UnitPrice, item.Quantity, subtotal)
	}

	res := map[string]interface{}{
		"result":      "OK",
		"orderNo":     orderNo,
		"orderStatus": status,
		"totalAmount": req.TotalAmount,
		"message":     "注文を受け付けました",
	}

	writeLog("入電文", req)
	writeLog("出電文", res)

	json.NewEncoder(w).Encode(res)
}

func validate(req OrderRequest) string {
	if req.TerminalNo == "" {
		return "terminalNo は必須です"
	}
	if req.MessageType != "ORDER_CONFIRM" {
		return "messageType は ORDER_CONFIRM にしてください"
	}
	if req.TotalAmount < 1 {
		return "totalAmount は1以上です"
	}
	if len(req.Items) < 1 || len(req.Items) > 5 {
		return "items は1〜5件です"
	}

	total := 0
	menus := map[string]bool{}

	for _, item := range req.Items {
		if item.MenuName == "" {
			return "menuName は必須です"
		}
		if menus[item.MenuName] {
			return "menuName が重複しています"
		}
		menus[item.MenuName] = true

		if item.UnitPrice < 1 {
			return "unitPrice は1以上です"
		}
		if item.Quantity < 1 || item.Quantity > 5 {
			return "quantity は1〜5です"
		}

		total += item.UnitPrice * item.Quantity
	}

	if total != req.TotalAmount {
		return fmt.Sprintf("totalAmount が一致しません。正しくは %d です", total)
	}

	return ""
}

func createOrderNo(db *sql.DB) string {
	prefix := time.Now().Format("0102") + "-"

	var count int
	db.QueryRow(
		"SELECT COUNT(DISTINCT order_no) FROM order_items WHERE order_no LIKE ?",
		prefix+"%",
	).Scan(&count)

	return fmt.Sprintf("%s%03d", prefix, count+1)
}

func getOrders(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	status := r.URL.Query().Get("status")

	var rows *sql.Rows
	if status == "" {
		rows, _ = db.Query(`
SELECT order_no, terminal_no, order_status, SUM(subtotal), MIN(created_at)
FROM order_items
GROUP BY order_no, terminal_no, order_status
ORDER BY MIN(created_at) DESC`)
	} else {
		rows, _ = db.Query(`
SELECT order_no, terminal_no, order_status, SUM(subtotal), MIN(created_at)
FROM order_items
WHERE order_status = ?
GROUP BY order_no, terminal_no, order_status
ORDER BY MIN(created_at) DESC`, status)
	}
	defer rows.Close()

	list := []map[string]interface{}{}

	for rows.Next() {
		var orderNo, terminalNo, orderStatus, createdAt string
		var total int

		rows.Scan(&orderNo, &terminalNo, &orderStatus, &total, &createdAt)

		list = append(list, map[string]interface{}{
			"orderNo":     orderNo,
			"terminalNo":  terminalNo,
			"orderStatus": orderStatus,
			"totalAmount": total,
			"createdAt":   createdAt,
		})
	}

	json.NewEncoder(w).Encode(list)
}

func getOrderDetail(w http.ResponseWriter, db *sql.DB, orderNo string) {
	rows, _ := db.Query(`
SELECT item_no, menu_name, unit_price, quantity, subtotal, order_status, created_at
FROM order_items
WHERE order_no = ?
ORDER BY item_no`, orderNo)
	defer rows.Close()

	items := []map[string]interface{}{}

	for rows.Next() {
		var itemNo, unitPrice, quantity, subtotal int
		var menuName, status, createdAt string

		rows.Scan(&itemNo, &menuName, &unitPrice, &quantity, &subtotal, &status, &createdAt)

		items = append(items, map[string]interface{}{
			"itemNo":      itemNo,
			"menuName":    menuName,
			"unitPrice":   unitPrice,
			"quantity":    quantity,
			"subtotal":    subtotal,
			"orderStatus": status,
			"createdAt":   createdAt,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"orderNo": orderNo,
		"items":   items,
	})
}

func updateStatus(w http.ResponseWriter, r *http.Request, db *sql.DB, orderNo string) {
	var req StatusRequest
	json.NewDecoder(r.Body).Decode(&req)

	if req.OrderStatus != "オーダー受信" &&
		req.OrderStatus != "クッキング終了" &&
		req.OrderStatus != "受け渡し終了" {
		http.Error(w, "orderStatus が不正です", http.StatusBadRequest)
		return
	}

	db.Exec("UPDATE order_items SET order_status = ? WHERE order_no = ?", req.OrderStatus, orderNo)

	res := map[string]interface{}{
		"result":      "OK",
		"orderNo":     orderNo,
		"orderStatus": req.OrderStatus,
		"message":     "注文状態を更新しました",
	}

	writeLog("DB更新内容", res)
	json.NewEncoder(w).Encode(res)
}

func writeLog(title string, data interface{}) {
	os.MkdirAll("logs", 0755)

	file, _ := os.OpenFile("logs/order.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	defer file.Close()

	b, _ := json.MarshalIndent(data, "", "  ")

	file.WriteString("========== " + title + " ==========\n")
	file.WriteString(time.Now().Format("2006-01-02 15:04:05") + "\n")
	file.WriteString(string(b) + "\n\n")
}

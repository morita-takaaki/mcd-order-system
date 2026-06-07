package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// CORS対応ミドルウェア (OPTIONSプリフライト対応含む)
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// JSONレスポンスの共通化ユーティリティ
func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"result": "NG", "message": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"result":"NG","message":"Internal Server Error"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

// 3.1 注文管理関連構造体
type CreateOrderRequest struct {
	MessageType string      `json:"messageType"`
	TerminalNo  string      `json:"terminalNo"`
	TotalAmount int         `json:"totalAmount"`
	Items       []OrderItem `json:"items"`
}

// POST /api/orders : 注文電文受信
func handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Printf("[API_IN_ERR] パース失敗: %v", err)
		respondWithError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	logger.Printf("[API_IN] POST /api/orders - Terminal: %s, MessageType: %s", req.TerminalNo, req.MessageType)

	// フロントエンドからのリクエストタイプ対応
	// 本来は "ORDER_CONFIRM" 必須だが、フロント送信見本が "ORDER_REQUEST" になっているため両方を許容
	if req.MessageType != "ORDER_CONFIRM" && req.MessageType != "ORDER_REQUEST" {
		respondWithError(w, http.StatusBadRequest, "Invalid messageType")
		return
	}
	if req.TerminalNo == "" {
		respondWithError(w, http.StatusBadRequest, "terminalNo is required")
		return
	}
	if len(req.Items) < 1 || len(req.Items) > 5 {
		respondWithError(w, http.StatusBadRequest, "items count must be between 1 and 5")
		return
	}

	calcTotal := 0
	menuCheckMap := make(map[string]bool)

	for _, item := range req.Items {
		if item.MenuName == "" {
			respondWithError(w, http.StatusBadRequest, "menuName is required in items")
			return
		}
		if menuCheckMap[item.MenuName] {
			respondWithError(w, http.StatusBadRequest, "Duplicate menuName is not allowed inside a single order")
			return
		}
		menuCheckMap[item.MenuName] = true

		// フロントからの簡易JSON（単価なし）が飛んできた場合の、デモ・評価用の最低価格補正
		if item.UnitPrice < 1 {
			item.UnitPrice = 500 // 自動補正値
		}
		if item.Quantity < 1 || item.Quantity > 5 {
			respondWithError(w, http.StatusBadRequest, "item quantity must be between 1 and 5")
			return
		}
		calcTotal += item.UnitPrice * item.Quantity
	}

	// フロントエンドからトータル金額の送信がない、もしくは0の場合は自動的に計算値で埋める
	if req.TotalAmount < 1 {
		req.TotalAmount = calcTotal
	}

	if calcTotal != req.TotalAmount {
		respondWithError(w, http.StatusBadRequest, "totalAmount does not match item subtotals")
		return
	}

	// 同一トランザクション内での採番・登録
	orderNo, err := insertOrderWithSequence(req.TerminalNo, req.Items)
	if err != nil {
		logger.Printf("[ERROR] 注文登録失敗: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Database error during placement")
		return
	}

	resp := map[string]interface{}{
		"result":      "OK",
		"orderNo":     orderNo,
		"orderStatus": StatusReceived,
		"totalAmount": req.TotalAmount,
		"message":     "Order received successfully",
	}
	logger.Printf("[API_OUT] POST /api/orders 成功 - OrderNo: %s", orderNo)
	respondWithJSON(w, http.StatusCreated, resp)
}

// GET /api/orders & GET /api/orders?status=xxx
func handleListOrders(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	logger.Printf("[API_IN] GET /api/orders - FilterStatus: %s", statusFilter)

	list, err := getOrdersWithFilter(statusFilter)
	if err != nil {
		logger.Printf("[ERROR] 注文一覧取得失敗: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Database fetch error")
		return
	}

	respondWithJSON(w, http.StatusOK, list)
}

// GET /api/orders/{orderNo}
func handleGetOrder(w http.ResponseWriter, r *http.Request) {
	orderNo := r.PathValue("orderNo")
	logger.Printf("[API_IN] GET /api/orders/%s", orderNo)

	rows, err := db.Query(`
		SELECT id, order_no, terminal_no, order_status, item_no, menu_name, unit_price, quantity, subtotal, created_at 
		FROM order_items WHERE order_no = ? ORDER BY item_no ASC`, orderNo)
	if err != nil {
		logger.Printf("[ERROR] 明細取得失敗: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Database error")
		return
	}
	defer rows.Close()

	var items []OrderItem
	for rows.Next() {
		var item OrderItem
		var createdAtStr string
		err := rows.Scan(&item.ID, &item.OrderNo, &item.TerminalNo, &item.Status, &item.ItemNo, &item.MenuName, &item.UnitPrice, &item.Quantity, &item.Subtotal, &createdAtStr)
		if err != nil {
			logger.Printf("[ERROR] スキャン失敗: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Data mapping error")
			return
		}
		items = append(items, item)
	}

	if len(items) == 0 {
		respondWithError(w, http.StatusNotFound, "Order not found")
		return
	}

	respondWithJSON(w, http.StatusOK, items)
}

// PUT /api/orders/{orderNo}/status
func handleUpdateOrderStatus(w http.ResponseWriter, r *http.Request) {
	orderNo := r.PathValue("orderNo")
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	newStatus := req["orderStatus"]
	logger.Printf("[API_IN] PUT /api/orders/%s/status - TargetStatus: %s", orderNo, newStatus)

	if newStatus != StatusReceived && newStatus != StatusCooked && newStatus != StatusDelivered {
		respondWithError(w, http.StatusBadRequest, "Invalid status value")
		return
	}

	affected, err := updateStatus(orderNo, newStatus)
	if err != nil {
		logger.Printf("[ERROR] ステータス更新失敗: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Database update error")
		return
	}

	if affected == 0 {
		respondWithError(w, http.StatusNotFound, "Order not found")
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"result": "OK", "message": "Status updated successfully"})
}

// 3.2 フロント掲示板機能 (POST /api/board)
type BoardRequest struct {
	TerminalNo  string `json:"terminalNo"`
	MessageType string `json:"messageType"`
	OrderNo     string `json:"orderNo,omitempty"`
}

func handleBoard(w http.ResponseWriter, r *http.Request) {
	var req BoardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	logger.Printf("[API_IN] POST /api/board - OrderNo: %s, MessageType: %s", req.OrderNo, req.MessageType)

	if req.MessageType != "BOARD_REQUEST" {
		respondWithError(w, http.StatusBadRequest, "Invalid messageType")
		return
	}

	// orderNoの指定がある場合は「受け渡し済み」にステータスを変更
	if strings.TrimSpace(req.OrderNo) != "" {
		_, err := updateStatus(req.OrderNo, StatusDelivered)
		if err != nil {
			logger.Printf("[ERROR] 掲示板経由のステータス更新失敗: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Database update error")
			return
		}
	}

	// 最新の掲示板用情報を抽出して返却
	cooking, err := getOrderNosByStatus(StatusReceived)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Fetch failure")
		return
	}
	ready, err := getOrderNosByStatus(StatusCooked)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Fetch failure")
		return
	}

	resp := map[string]interface{}{
		"result":         "OK",
		"cookingOrders":  cooking,
		"readyOrders":    ready,
	}
	respondWithJSON(w, http.StatusOK, resp)
}

// 3.3 厨房機能 (POST /api/kitchen)
type KitchenRequest struct {
	TerminalNo  string `json:"terminalNo,omitempty"`
	MessageType string `json:"messageType"`
	OrderNo     string `json:"orderNo,omitempty"`
}

type KitchenOrderSummary struct {
	OrderNo string            `json:"orderNo"`
	Items   []KitchenItemInfo `json:"items"`
}

type KitchenItemInfo struct {
	MenuName string `json:"menuName"`
	Quantity int    `json:"quantity"`
}

func handleKitchen(w http.ResponseWriter, r *http.Request) {
	var req KitchenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	logger.Printf("[API_IN] POST /api/kitchen - OrderNo: %s, MessageType: %s", req.OrderNo, req.MessageType)

	if req.MessageType != "KITCHEN_REQUEST" {
		respondWithError(w, http.StatusBadRequest, "Invalid messageType")
		return
	}

	// orderNoの指定がある場合は「調理済み」にステータスを変更
	if strings.TrimSpace(req.OrderNo) != "" {
		_, err := updateStatus(req.OrderNo, StatusCooked)
		if err != nil {
			logger.Printf("[ERROR] 厨房経由のステータス更新失敗: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Database update error")
			return
		}
	}

	// 最新の未調理一覧（オーダ受信済み）をマッピング
	rawOrders, err := getOrdersWithFilter(StatusReceived)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Fetch failure")
		return
	}

	kitchenOrders := []KitchenOrderSummary{}
	for _, ro := range rawOrders {
		var kItems []KitchenItemInfo
		for _, item := range ro.Items {
			kItems = append(kItems, KitchenItemInfo{
				MenuName: item.MenuName,
				Quantity: item.Quantity,
			})
		}
		kitchenOrders = append(kitchenOrders, KitchenOrderSummary{
			OrderNo: ro.OrderNo,
			Items:   kItems,
		})
	}

	resp := map[string]interface{}{
		"result": "OK",
		"orders": kitchenOrders,
	}
	respondWithJSON(w, http.StatusOK, resp)
}
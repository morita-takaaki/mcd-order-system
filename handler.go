package main

import (
	"encoding/json"
	"net/http"
)

type OrderItemInput struct {
	MenuName  string `json:"menuName"`
	UnitPrice int    `json:"unitPrice"`
	Quantity  int    `json:"quantity"`
	Subtotal  int    `json:"subtotal"`
}

type OrderConfirmRequest struct {
	MessageType string           `json:"messageType"`
	TerminalNo  string           `json:"terminalNo"`
	TotalAmount int              `json:"totalAmount"`
	Items       []OrderItemInput `json:"items"`
}

type GenericResponse struct {
	Result        string   `json:"result"`
	OrderNo       string   `json:"orderNo,omitempty"`
	OrderStatus   string   `json:"orderStatus,omitempty"`
	TotalAmount   int      `json:"totalAmount,omitempty"`
	Message       string   `json:"message,omitempty"`
	CookingOrders []string `json:"cookingOrders,omitempty"`
	ReadyOrders   []string `json:"readyOrders,omitempty"`
}

type OrderItemResponse struct {
	MenuName string `json:"menuName"`
	Quantity int    `json:"quantity"`
}

type KitchenOrderResponse struct {
	OrderNo string              `json:"orderNo"`
	Items   []OrderItemResponse `json:"items"`
}

type KitchenResponse struct {
	Result string                 `json:"result"`
	Orders []KitchenOrderResponse `json:"orders"`
}

type BoardKitchenRequest struct {
	MessageType string `json:"messageType"`
	TerminalNo  string `json:"terminalNo"`
	OrderNo     string `json:"orderNo,omitempty"`
}

func handleOrdersPost(w http.ResponseWriter, r *http.Request) {
	var req OrderConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "Invalid JSON structure")
		return
	}

	reqBytes, _ := json.Marshal(req)
	Logger.Printf("[API入電文 POST /api/orders] %s", string(reqBytes))

	// バリデーションチェック
	if req.TerminalNo == "" || req.MessageType != "ORDER_CONFIRM" || req.TotalAmount < 1 {
		respondError(w, "Validation Error: Check terminalNo, messageType, or totalAmount")
		return
	}
	if len(req.Items) < 1 || len(req.Items) > 5 {
		respondError(w, "Validation Error: items list length must be 1 to 5")
		return
	}

	calcTotal := 0
	menuSet := make(map[string]bool)
	for _, item := range req.Items {
		if item.MenuName == "" || item.UnitPrice < 1 || item.Quantity < 1 || item.Quantity > 5 {
			respondError(w, "Validation Error: Invalid item specifications")
			return
		}
		if menuSet[item.MenuName] {
			respondError(w, "Validation Error: Duplicate menuName forbidden in single order")
			return
		}
		menuSet[item.MenuName] = true

		calcSubtotal := item.UnitPrice * item.Quantity
		if item.Subtotal != calcSubtotal {
			respondError(w, "Validation Error: Calculated subtotal mismatch")
			return
		}
		calcTotal += calcSubtotal
	}

	if req.TotalAmount != calcTotal {
		respondError(w, "Validation Error: totalAmount mismatch with summation of subtotals")
		return
	}

	orderNo, err := createOrderInTx(req.TerminalNo, req.Items)
	if err != nil {
		respondError(w, "Internal Database Transaction Failure: "+err.Error())
		return
	}

	res := GenericResponse{
		Result:  "OK",
		OrderNo: orderNo,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resBytes, _ := json.Marshal(res)
	Logger.Printf("[API出電文 POST /api/orders] %s", string(resBytes))
	w.Write(resBytes)
}

func handleOrdersGet(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	Logger.Printf("[API入電文 GET /api/orders?status=%s]", statusFilter)

	query := "SELECT order_no, order_status FROM order_items"
	var args []interface{}
	if statusFilter != "" {
		query += " WHERE order_status = ?"
		args = append(args, statusFilter)
	}
	query += " GROUP BY order_no ORDER BY id ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type OrderSummary struct {
		OrderNo     string `json:"orderNo"`
		OrderStatus string `json:"orderStatus"`
	}
	orders := []OrderSummary{}
	for rows.Next() {
		var o OrderSummary
		if err := rows.Scan(&o.OrderNo, &o.OrderStatus); err == nil {
			orders = append(orders, o)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}

func handleOrdersGetByNo(w http.ResponseWriter, r *http.Request) {
	orderNo := r.PathValue("orderNo")
	Logger.Printf("[API入電文 GET /api/orders/%s]", orderNo)

	rows, err := db.Query("SELECT menu_name, quantity FROM order_items WHERE order_no = ?", orderNo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := []OrderItemResponse{}
	for rows.Next() {
		var item OrderItemResponse
		if err := rows.Scan(&item.MenuName, &item.Quantity); err == nil {
			items = append(items, item)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func handleOrdersStatusPut(w http.ResponseWriter, r *http.Request) {
	orderNo := r.PathValue("orderNo")
	var body struct {
		OrderStatus string `json:"orderStatus"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	Logger.Printf("[API入電文 PUT /api/orders/%s/status] status: %s", orderNo, body.OrderStatus)

	_, err := db.Exec("UPDATE order_items SET order_status = ? WHERE order_no = ?", body.OrderStatus, orderNo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	Logger.Printf("[DB更新内容] OrderNo: %s status updated to %s", orderNo, body.OrderStatus)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"result":"OK"}`))
}

func handleBoardPost(w http.ResponseWriter, r *http.Request) {
	var req BoardKitchenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	reqBytes, _ := json.Marshal(req)
	Logger.Printf("[API入電文 POST /api/board] %s", string(reqBytes))

	if req.MessageType != "BOARD_REQUEST" {
		respondError(w, "Validation Error: messageType must be BOARD_REQUEST")
		return
	}

	if req.OrderNo != "" {
		_, err := db.Exec("UPDATE order_items SET order_status = '受け渡し済み' WHERE order_no = ?", req.OrderNo)
		if err != nil {
			respondError(w, "Failed to update status to 受け渡し済み")
			return
		}
		Logger.Printf("[DB更新内容] OrderNo: %s status updated to 受け渡し済み", req.OrderNo)
	}

	cookingRows, _ := db.Query("SELECT DISTINCT order_no FROM order_items WHERE order_status = 'オーダ受信済み' ORDER BY id ASC")
	cookingOrders := []string{}
	for cookingRows.Next() {
		var no string
		if err := cookingRows.Scan(&no); err == nil {
			cookingOrders = append(cookingOrders, no)
		}
	}
	cookingRows.Close()

	readyRows, _ := db.Query("SELECT DISTINCT order_no FROM order_items WHERE order_status = '調理済み' ORDER BY id ASC")
	readyOrders := []string{}
	for readyRows.Next() {
		var no string
		if err := readyRows.Scan(&no); err == nil {
			readyOrders = append(readyOrders, no)
		}
	}
	readyRows.Close()

	res := GenericResponse{
		Result:        "OK",
		CookingOrders: cookingOrders,
		ReadyOrders:   readyOrders,
	}

	w.Header().Set("Content-Type", "application/json")
	resBytes, _ := json.Marshal(res)
	Logger.Printf("[API出電文 POST /api/board] %s", string(resBytes))
	w.Write(resBytes)
}

func handleKitchenPost(w http.ResponseWriter, r *http.Request) {
	var req BoardKitchenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	reqBytes, _ := json.Marshal(req)
	Logger.Printf("[API入電文 POST /api/kitchen] %s", string(reqBytes))

	if req.MessageType != "KITCHEN_REQUEST" {
		respondError(w, "Validation Error: messageType must be KITCHEN_REQUEST")
		return
	}

	if req.OrderNo != "" {
		_, err := db.Exec("UPDATE order_items SET order_status = '調理済み' WHERE order_no = ?", req.OrderNo)
		if err != nil {
			respondError(w, "Failed to update status to 調理済み")
			return
		}
		Logger.Printf("[DB更新内容] OrderNo: %s status updated to 調理済み", req.OrderNo)
	}

	rows, err := db.Query("SELECT order_no, menu_name, quantity FROM order_items WHERE order_status = 'オーダ受信済み' ORDER BY id ASC")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	orderMap := make(map[string][]OrderItemResponse)
	orderOrder := []string{}

	for rows.Next() {
		var orderNo, menuName string
		var qty int
		if err := rows.Scan(&orderNo, &menuName, &qty); err == nil {
			if _, exists := orderMap[orderNo]; !exists {
				orderOrder = append(orderOrder, orderNo)
			}
			orderMap[orderNo] = append(orderMap[orderNo], OrderItemResponse{MenuName: menuName, Quantity: qty})
		}
	}

	ordersRes := []KitchenOrderResponse{}
	for _, no := range orderOrder {
		ordersRes = append(ordersRes, KitchenOrderResponse{
			OrderNo: no,
			Items:   orderMap[no],
		})
	}

	res := KitchenResponse{
		Result: "OK",
		Orders: ordersRes,
	}

	w.Header().Set("Content-Type", "application/json")
	resBytes, _ := json.Marshal(res)
	Logger.Printf("[API出電文 POST /api/kitchen] %s", string(resBytes))
	w.Write(resBytes)
}

func respondError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	res, _ := json.Marshal(map[string]string{"result": "NG", "message": msg})
	Logger.Printf("[API出電文 エラーレスポンス] %s", string(res))
	w.Write(res)
}
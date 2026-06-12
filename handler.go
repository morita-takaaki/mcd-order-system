package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

// --- 構造体定義 ---

// 注文API用
type OrderItem struct {
	MenuName  string `json:"menuName"`
	UnitPrice int    `json:"unitPrice"`
	Quantity  int    `json:"quantity"`
	Subtotal  int    `json:"subtotal"`
}

type OrderRequest struct {
	MessageType string      `json:"messageType"`
	TerminalNo  string      `json:"terminalNo"`
	TotalAmount int         `json:"totalAmount"`
	Items       []OrderItem `json:"items"`
}

// 厨房API用
type KitchenRequest struct {
	MessageType string `json:"messageType"`
	TerminalNo  string `json:"terminalNo,omitempty"`
	OrderNo     string `json:"orderNo,omitempty"`
}

type KitchenOrderItem struct {
	MenuName string `json:"menuName"`
	Quantity int    `json:"quantity"`
}

type KitchenOrder struct {
	OrderNo string             `json:"orderNo"`
	Items   []KitchenOrderItem `json:"items"`
}

type KitchenResponse struct {
	Result string         `json:"result"`
	Orders []KitchenOrder `json:"orders"`
}

// 掲示板API用
type BoardRequest struct {
	MessageType string `json:"messageType"`
	TerminalNo  string `json:"terminalNo,omitempty"`
	OrderNo     string `json:"orderNo,omitempty"`
}

type BoardResponse struct {
	Result        string   `json:"result"`
	CookingOrders []string `json:"cookingOrders"`
	ReadyOrders   []string `json:"readyOrders"`
}

// ログ出力用構造体
type LogOrderItem struct {
	OrderNo     string `json:"order_no"`
	TerminalNo  string `json:"terminal_no"`
	OrderStatus string `json:"order_status"`
	ItemNo      int    `json:"item_no"`
	MenuName    string `json:"menu_name"`
	UnitPrice   int    `json:"unit_price"`
	Quantity    int    `json:"quantity"`
	Subtotal    int    `json:"subtotal"`
}

// エラーレスポンス共通用
type ErrorResponse struct {
	Result  string `json:"result"`
	Message string `json:"message"`
}

// --- ハンドラー実装 ---

func handleOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}

	log.Printf("[API入電文][POST /api/orders]: %s", string(bodyBytes))

	var req OrderRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "JSON Parse Error")
		return
	}

	if req.MessageType != "ORDER_CONFIRM" {
		respondWithError(w, http.StatusBadRequest, "Invalid messageType")
		return
	}

	// DB登録処理
	orderNo, loggedItems, err := insertOrder(req)
	if err != nil {
		log.Printf("[DB登録エラー]: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	// DB登録内容のログ出力
	for _, item := range loggedItems {
		itemJSON, _ := json.Marshal(item)
		log.Printf("[DB登録内容][order_items]: %s", string(itemJSON))
	}

	// レスポンス作成
	resp := map[string]string{"result": "OK", "orderNo": orderNo}
	respBytes, _ := json.Marshal(resp)
	
	log.Printf("[API出電文][POST /api/orders]: %s", string(respBytes))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBytes)
}

func handleKitchen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}

	log.Printf("[API入電文][POST /api/kitchen]: %s", string(bodyBytes))

	var req KitchenRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "JSON Parse Error")
		return
	}

	if req.MessageType != "KITCHEN_REQUEST" {
		respondWithError(w, http.StatusBadRequest, "Invalid messageType")
		return
	}

	// orderNoが指定されている場合は更新処理
	if req.OrderNo != "" {
		rowsAffected, err := updateKitchenStatus(req.OrderNo)
		if err != nil {
			log.Printf("[DB更新エラー]: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		log.Printf("[DB更新内容][order_items]: order_no=%s, order_status='調理済み'に更新 (影響件数: %d)", req.OrderNo, rowsAffected)
	}

	// 最新の未調理（オーダ受信済み）一覧を取得して返却
	orders, err := getKitchenOrders()
	if err != nil {
		log.Printf("[DB参照エラー]: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	resp := KitchenResponse{
		Result: "OK",
		Orders: orders,
	}
	respBytes, _ := json.Marshal(resp)

	log.Printf("[API出電文][POST /api/kitchen]: %s", string(respBytes))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBytes)
}

func handleBoard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}

	log.Printf("[API入電文][POST /api/board]: %s", string(bodyBytes))

	var req BoardRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "JSON Parse Error")
		return
	}

	if req.MessageType != "BOARD_REQUEST" {
		respondWithError(w, http.StatusBadRequest, "Invalid messageType")
		return
	}

	// orderNoが指定されている場合は更新処理
	if req.OrderNo != "" {
		rowsAffected, err := updateBoardStatus(req.OrderNo)
		if err != nil {
			log.Printf("[DB更新エラー]: %v", err)
			respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		log.Printf("[DB更新内容][order_items]: order_no=%s, order_status='受け渡し済み'に更新 (影響件数: %d)", req.OrderNo, rowsAffected)
	}

	// 最新の掲示板用情報をマッピングして取得
	cookingOrders, readyOrders, err := getBoardOrders()
	if err != nil {
		log.Printf("[DB参照エラー]: %v", err)
		respondWithError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	// nilの場合にJSONで[]になるよう初期化
	if cookingOrders == nil {
		cookingOrders = []string{}
	}
	if readyOrders == nil {
		readyOrders = []string{}
	}

	resp := BoardResponse{
		Result:        "OK",
		CookingOrders: cookingOrders,
		ReadyOrders:   readyOrders,
	}
	respBytes, _ := json.Marshal(resp)

	log.Printf("[API出電文][POST /api/board]: %s", string(respBytes))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBytes)
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	resp := ErrorResponse{Result: "NG", Message: message}
	respBytes, _ := json.Marshal(resp)
	log.Printf("[API出電文][ERROR %d]: %s", code, string(respBytes))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(respBytes)
}
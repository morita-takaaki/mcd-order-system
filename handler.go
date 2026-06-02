package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

// --- リクエスト・レスポンス用構造体定義 ---

type OrderItemInput struct {
	MenuName  string `json:"menuName"`
	UnitPrice int    `json:"unitPrice"`
	Quantity  int    `json:"quantity"`
}

type PostOrderRequest struct {
	MessageType string           `json:"messageType"`
	TerminalNo  string           `json:"terminalNo"`
	TotalAmount int              `json:"totalAmount"`
	Items       []OrderItemInput `json:"items"`
}

type PostOrderResponse struct {
	Result      string `json:"result"`
	OrderNo     string `json:"orderNo,omitempty"`
	OrderStatus string `json:"orderStatus,omitempty"`
	TotalAmount int    `json:"totalAmount,omitempty"`
	Message     string `json:"message"`
}

type OrderItemOutput struct {
	ItemNo    int    `json:"itemNo"`
	MenuName  string `json:"menuName"`
	UnitPrice int    `json:"unitPrice"`
	Quantity  int    `json:"quantity"`
	Subtotal  int    `json:"subtotal"`
}

type OrderSummaryResponse struct {
	OrderNo     string            `json:"orderNo"`
	TerminalNo  string            `json:"terminalNo"`
	OrderStatus string            `json:"orderStatus"`
	TotalAmount int               `json:"totalAmount"`
	CreatedAt   string            `json:"createdAt"`
	Items       []OrderItemOutput `json:"items"`
}

type PutStatusRequest struct {
	OrderStatus string `json:"orderStatus"`
}

type CommonResponse struct {
	Result  string `json:"result"`
	Message string `json:"message"`
}

type BoardRequest struct {
	TerminalNo  string `json:"terminalNo"`
	MessageType string `json:"messageType"`
	OrderNo     string `json:"orderNo,omitempty"` // オプション項目
}

type BoardResponse struct {
	Result        string   `json:"result"`
	CookingOrders []string `json:"cookingOrders"`
	ReadyOrders   []string `json:"readyOrders"`
}

type KitchenRequest struct {
	TerminalNo  string `json:"terminalNo"`
	MessageType string `json:"messageType"`
	OrderNo     string `json:"orderNo,omitempty"` // オプション項目
}

type KitchenItem struct {
	MenuName string `json:"menuName"`
	Quantity int    `json:"quantity"`
}

type KitchenOrder struct {
	OrderNo string        `json:"orderNo"`
	Items   []KitchenItem `json:"items"`
}

type KitchenResponse struct {
	Result string         `json:"result"`
	Orders []KitchenOrder `json:"orders"`
}

// --- CORS対応共通ミドルウェア ---
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 全てのオリジンからのアクセスを許可
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// プリフライト（OPTIONS）リクエストの場合は、ここでレスポンスを返す
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// 共通JSONレスポンス送信関数
func respondWithJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[ERROR] JSONのエンコードに失敗しました: %v", err)
		return
	}
	// 出電文（レスポンス）をログ出力
	log.Printf("[API_OUT] Status: %d, Response: %s\n", status, string(jsonBytes))
	w.Write(jsonBytes)
}

// 共通エラーレスポンス作成関数
func respondWithError(w http.ResponseWriter, status int, msg string) {
	respondWithJSON(w, status, CommonResponse{Result: "NG", Message: msg})
}

// 3.1 注文管理機能: POST /api/orders (注文電文受信)
func handlePostOrders(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "リクエストボディの読み込みに失敗しました。")
		return
	}
	// 入電文（リクエスト）をログ出力
	log.Printf("[API_IN] POST /api/orders, Body: %s\n", string(bodyBytes))

	var req PostOrderRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "JSON形式が不正です。")
		return
	}

	// 入力チェック（バリデーション）
	if req.TerminalNo == "" {
		respondWithError(w, http.StatusBadRequest, "terminalNo は必須項目です。")
		return
	}
	if req.MessageType != "ORDER_CONFIRM" {
		respondWithError(w, http.StatusBadRequest, "messageType は 'ORDER_CONFIRM' である必要があります。")
		return
	}
	if req.TotalAmount < 1 {
		respondWithError(w, http.StatusBadRequest, "totalAmount は 1 以上である必要があります。")
		return
	}
	if len(req.Items) < 1 || len(req.Items) > 5 {
		respondWithError(w, http.StatusBadRequest, "items の件数は 1〜5 件である必要があります。")
		return
	}

	// 明細行の検証用変数
	calculatedTotal := 0
	menuNameMap := make(map[string]bool)

	for idx, item := range req.Items {
		if item.MenuName == "" {
			respondWithError(w, http.StatusBadRequest, "明細の menuName は必須項目です。")
			return
		}
		if item.UnitPrice < 1 {
			respondWithError(w, http.StatusBadRequest, "明細の unitPrice は 1 以上である必要があります。")
			return
		}
		if item.Quantity < 1 || item.Quantity > 5 {
			respondWithError(w, http.StatusBadRequest, "明細の quantity は 1〜5 の間である必要があります。")
			return
		}

		// メニュー名の重複チェック
		if menuNameMap[item.MenuName] {
			respondWithError(w, http.StatusBadRequest, "同一注文内での同一 menuName (メニュー名) の重複登録は禁止されています: "+item.MenuName)
			return
		}
		menuNameMap[item.MenuName] = true

		// 小計の加算計算
		subtotal := item.UnitPrice * item.Quantity
		calculatedTotal += subtotal
		_ = idx // ログ表示などで利用可能
	}

	// 自動計算された合計金額とリクエストのtotalAmountの不一致チェック
	if calculatedTotal != req.TotalAmount {
		respondWithError(w, http.StatusBadRequest, "明細の小計合計が totalAmount と一致しません。")
		return
	}

	// トランザクションを伴う自動採番・インサート処理の呼び出し
	newOrderNo, err := insertOrderWithSequence(req.TerminalNo, req.Items)
	if err != nil {
		log.Printf("[ERROR] DB登録処理でエラーが発生しました: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "データベースへの登録処理に失敗しました。")
		return
	}

	// 成功レスポンスの返却
	response := PostOrderResponse{
		Result:      "OK",
		OrderNo:     newOrderNo,
		OrderStatus: "オーダ受信済み",
		TotalAmount: req.TotalAmount,
		Message:     "注文を正常に受け付けました。",
	}
	respondWithJSON(w, http.StatusCreated, response)
}

// 3.1 注文管理機能: GET /api/orders (注文一覧取得・状態別一覧取得)
func handleGetOrders(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API_IN] GET /api/orders, Query: %s\n", r.URL.RawQuery)

	// クエリパラメータの取得 (?status=xxx)
	statusFilter := r.URL.Query().Get("status")

	// DBから条件に合うすべての明細行を取得
	dbItems, err := fetchAllOrderItems(statusFilter)
	if err != nil {
		log.Printf("[ERROR] DBからの注文一覧取得に失敗しました: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "データ取得に失敗しました。")
		return
	}

	// 同一 order_no の複数明細を1つの注文オブジェクトとして集約するロジック
	var orderList []OrderSummaryResponse
	orderIndexMap := make(map[string]int) // order_no -> orderListのインデックス構造

	for _, item := range dbItems {
		idx, exists := orderIndexMap[item.OrderNo]
		outputItem := OrderItemOutput{
			ItemNo:    item.ItemNo,
			MenuName:  item.MenuName,
			UnitPrice: item.UnitPrice,
			Quantity:  item.Quantity,
			Subtotal:  item.Subtotal,
		}

		if !exists {
			// 新しい注文番号を発見した場合、新規に集約オブジェクトを作成
			summary := OrderSummaryResponse{
				OrderNo:     item.OrderNo,
				TerminalNo:  item.TerminalNo,
				OrderStatus: item.OrderStatus,
				TotalAmount: item.Subtotal, // 最初の明細の小計で初期化
				CreatedAt:   item.CreatedAt.Format("2006-01-02 15:04:05"),
				Items:       []OrderItemOutput{outputItem},
			}
			orderList = append(orderList, summary)
			orderIndexMap[item.OrderNo] = len(orderList) - 1
		} else {
			// 既存の注文番号の場合、明細を追加し合計金額を加算
			orderList[idx].Items = append(orderList[idx].Items, outputItem)
			orderList[idx].TotalAmount += item.Subtotal
		}
	}

	// 抽出結果が空の場合は空配列（nullではなく[]）を返却するための措置
	if orderList == nil {
		orderList = []OrderSummaryResponse{}
	}

	respondWithJSON(w, http.StatusOK, orderList)
}

// 3.1 注文管理機能: GET /api/orders/{orderNo} (注文詳細取得)
func handleGetOrderByNo(w http.ResponseWriter, r *http.Request) {
	// Go 1.22パスパラメータ機能から orderNo を抽出
	orderNo := r.PathValue("orderNo")
	log.Printf("[API_IN] GET /api/orders/%s\n", orderNo)

	if orderNo == "" {
		respondWithError(w, http.StatusBadRequest, "注文番号が指定されていません。")
		return
	}

	dbItems, err := fetchOrderItemsByNo(orderNo)
	if err != nil {
		log.Printf("[ERROR] DBからの詳細データ取得に失敗しました: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "データ取得に失敗しました。")
		return
	}

	if len(dbItems) == 0 {
		respondWithError(w, http.StatusNotFound, "指定された注文番号は見つかりません。")
		return
	}

	// 単一注文オブジェクトへの集約
	var summary OrderSummaryResponse
	summary.OrderNo = dbItems[0].OrderNo
	summary.TerminalNo = dbItems[0].TerminalNo
	summary.OrderStatus = dbItems[0].OrderStatus
	summary.CreatedAt = dbItems[0].CreatedAt.Format("2006-01-02 15:04:05")
	summary.Items = []OrderItemOutput{}

	for _, item := range dbItems {
		outputItem := OrderItemOutput{
			ItemNo:    item.ItemNo,
			MenuName:  item.MenuName,
			UnitPrice: item.UnitPrice,
			Quantity:  item.Quantity,
			Subtotal:  item.Subtotal,
		}
		summary.Items = append(summary.Items, outputItem)
		summary.TotalAmount += item.Subtotal
	}

	respondWithJSON(w, http.StatusOK, summary)
}

// 3.1 注文管理機能: PUT /api/orders/{orderNo}/status (注文状態変更)
func handlePutOrderStatus(w http.ResponseWriter, r *http.Request) {
	orderNo := r.PathValue("orderNo")
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "リクエストボディの読み取りに失敗しました。")
		return
	}
	log.Printf("[API_IN] PUT /api/orders/%s/status, Body: %s\n", orderNo, string(bodyBytes))

	var req PutStatusRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "JSON形式が不正です。")
		return
	}

	// ステータスの値の検証
	if req.OrderStatus != "オーダ受信済み" && req.OrderStatus != "調理済み" && req.OrderStatus != "受け渡し済み" {
		respondWithError(w, http.StatusBadRequest, "不正なステータス値です。")
		return
	}

	rowsAffected, err := updateOrderStatus(orderNo, req.OrderStatus)
	if err != nil {
		log.Printf("[ERROR] ステータスの更新処理に失敗しました: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "状態変更に失敗しました。")
		return
	}

	if rowsAffected == 0 {
		respondWithError(w, http.StatusNotFound, "該当する注文番号が存在しません。")
		return
	}

	respondWithJSON(w, http.StatusOK, CommonResponse{Result: "OK", Message: "注文状態を正常に更新しました。"})
}

// 3.2 フロント掲示板機能: POST /api/board
func handlePostBoard(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "リクエストボディの読み込みに失敗しました。")
		return
	}
	log.Printf("[API_IN] POST /api/board, Body: %s\n", string(bodyBytes))

	var req BoardRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "JSON形式が不正です。")
		return
	}

	// 必須チェック
	if req.MessageType != "BOARD_REQUEST" {
		respondWithError(w, http.StatusBadRequest, "messageType は 'BOARD_REQUEST' である必要があります。")
		return
	}

	// orderNo が指定されている場合は「受け渡し完了処理」を実行
	if req.OrderNo != "" {
		_, err := updateOrderStatus(req.OrderNo, "受け渡し済み")
		if err != nil {
			log.Printf("[ERROR] 掲示板からの受け渡しステータス更新に失敗しました: %v\n", err)
			respondWithError(w, http.StatusInternalServerError, "ステータスの更新に失敗しました。")
			return
		}
	}

	// 最新の掲示板情報を参照・組み立てして返却
	allRawItems, err := fetchAllOrderItems("")
	if err != nil {
		log.Printf("[ERROR] 掲示板用データ取得に失敗しました: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "データ取得に失敗しました。")
		return
	}

	cookingMap := make(map[string]bool)
	readyMap := make(map[string]bool)
	var cookingOrders []string
	var readyOrders []string

	// 明細単位のデータを注文番号単位に集約かつ表示ルール変換を適用
	for _, item := range allRawItems {
		if item.OrderStatus == "オーダ受信済み" {
			if !cookingMap[item.OrderNo] {
				cookingMap[item.OrderNo] = true
				cookingOrders = append(cookingOrders, item.OrderNo)
			}
		} else if item.OrderStatus == "調理済み" {
			if !readyMap[item.OrderNo] {
				readyMap[item.OrderNo] = true
				readyOrders = append(readyOrders, item.OrderNo)
			}
		}
	}

	// nil排除の初期化
	if cookingOrders == nil {
		cookingOrders = []string{}
	}
	if readyOrders == nil {
		readyOrders = []string{}
	}

	respondWithJSON(w, http.StatusOK, BoardResponse{
		Result:        "OK",
		CookingOrders: cookingOrders,
		ReadyOrders:   readyOrders,
	})
}

// 3.3 厨房機能: POST /api/kitchen
func handlePostKitchen(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "リクエストボディの読み込みに失敗しました。")
		return
	}
	log.Printf("[API_IN] POST /api/kitchen, Body: %s\n", string(bodyBytes))

	var req KitchenRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "JSON形式が不正です。")
		return
	}

	// 必須チェック
	if req.MessageType != "KITCHEN_REQUEST" {
		respondWithError(w, http.StatusBadRequest, "messageType は 'KITCHEN_REQUEST' である必要があります。")
		return
	}

	// orderNo が指定されている場合は「調理完了処理」を実行
	if req.OrderNo != "" {
		_, err := updateOrderStatus(req.OrderNo, "調理済み")
		if err != nil {
			log.Printf("[ERROR] 厨房からの調理済みステータス更新に失敗しました: %v\n", err)
			respondWithError(w, http.StatusInternalServerError, "ステータスの更新に失敗しました。")
			return
		}
	}

	// 状態が「オーダ受信済み」の注文のみを対象として最新情報を取得
	kitchenRawItems, err := fetchAllOrderItems("オーダ受信済み")
	if err != nil {
		log.Printf("[ERROR] 厨房用データ取得に失敗しました: %v\n", err)
		respondWithError(w, http.StatusInternalServerError, "データ取得に失敗しました。")
		return
	}

	// 注文番号ごとに明細(メニュー名と数量)を集約
	var kitchenOrders []KitchenOrder
	orderIndexMap := make(map[string]int)

	for _, item := range kitchenRawItems {
		idx, exists := orderIndexMap[item.OrderNo]
		kItem := KitchenItem{
			MenuName: item.MenuName,
			Quantity: item.Quantity,
		}

		if !exists {
			kOrder := KitchenOrder{
				OrderNo: item.OrderNo,
				Items:   []KitchenItem{kItem},
			}
			kitchenOrders = append(kitchenOrders, kOrder)
			orderIndexMap[item.OrderNo] = len(kitchenOrders) - 1
		} else {
			kitchenOrders[idx].Items = append(kitchenOrders[idx].Items, kItem)
		}
	}

	if kitchenOrders == nil {
		kitchenOrders = []KitchenOrder{}
	}

	respondWithJSON(w, http.StatusOK, KitchenResponse{
		Result: "OK",
		Orders: kitchenOrders,
	})
}
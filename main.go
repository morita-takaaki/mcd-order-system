package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

var logger *log.Logger

func initLogger() {
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatalf("ログディレクトリの作成に失敗しました: %v", err)
	}

	logFile, err := os.OpenFile(filepath.Join(logDir, "order.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("ログファイルのオープンに失敗しました: %v", err)
	}

	// 標準出力とファイルの両方に出力
	mw := io.MultiWriter(os.Stdout, logFile)
	logger = log.New(mw, "", log.LstdFlags)
}

func main() {
	initLogger()
	logger.Println("[INFO] アプリケーションを起動しています...")

	// データベース初期化
	initDB()
	defer closeDB()

	// Go 1.22+ 標準マルチプレクサの利用
	mux := http.NewServeMux()

	// CORSミドルウェアを適用してルーティング登録
	mux.Handle("POST /api/orders", corsMiddleware(http.HandlerFunc(handleCreateOrder)))
	mux.Handle("GET /api/orders", corsMiddleware(http.HandlerFunc(handleListOrders)))
	mux.Handle("GET /api/orders/{orderNo}", corsMiddleware(http.HandlerFunc(handleGetOrder)))
	mux.Handle("PUT /api/orders/{orderNo}/status", corsMiddleware(http.HandlerFunc(handleUpdateOrderStatus)))
	mux.Handle("POST /api/board", corsMiddleware(http.HandlerFunc(handleBoard)))
	mux.Handle("POST /api/kitchen", corsMiddleware(http.HandlerFunc(handleKitchen)))

	server := &http.Server{
		Addr:    "0.0.0.0:8080",
		Handler: mux,
	}

	// グレースフルシャットダウンの実装
	go func() {
		logger.Printf("[INFO] サーバーが 0.0.0.0:8080 で起動しました。")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("[ERROR] サーバー起動失敗: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Println("[INFO] サーバーを停止しています...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatalf("[ERROR] サーバーの強制停止: %v", err)
	}
	logger.Println("[INFO] サーバーが正常に終了しました。")
}
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// グローバルなログライターとロガーの定義
var logFile *os.File

func initLogger() {
	logDir := "logs"
	// logs フォルダが存在しない場合は自動作成
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatalf("ログディレクトリの作成に失敗しました: %v", err)
	}

	logPath := filepath.Join(logDir, "order.log")
	// 追記モード、作成、書き込み専用でログファイルを開く
	var err error
	logFile, err = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("ログファイルのオープンに失敗しました: %v", err)
	}

	// 標準出力（コンソール）とファイル出力の両方に同時に出力する設定
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)
	// ログのプレフィックスに日付・時刻・マイクロ秒・ファイル名を表示
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
}

func main() {
	// ログの初期化
	initLogger()
	defer logFile.Close()

	log.Println("[INFO] アプリケーションの起動処理を開始します。")

	// データベースの初期化
	initDB()
	defer closeDB()

	// Go 1.22 以降の新しいマルチプレクサ（ServeMux）の作成
	mux := http.NewServeMux()

	// ルーティング定義 (標準のパス解析・メソッド指定機能を活用)
	// 共通CORSミドルウェアを適用して登録
	mux.Handle("POST /api/orders", corsMiddleware(http.HandlerFunc(handlePostOrders)))
	mux.Handle("GET /api/orders", corsMiddleware(http.HandlerFunc(handleGetOrders)))
	mux.Handle("GET /api/orders/{orderNo}", corsMiddleware(http.HandlerFunc(handleGetOrderByNo)))
	mux.Handle("PUT /api/orders/{orderNo}/status", corsMiddleware(http.HandlerFunc(handlePutOrderStatus)))
	mux.Handle("POST /api/board", corsMiddleware(http.HandlerFunc(handlePostBoard)))
	mux.Handle("POST /api/kitchen", corsMiddleware(http.HandlerFunc(handlePostKitchen)))

	// サーバーの設定 (0.0.0.0:8080 で Listen)
	serverAddr := "0.0.0.0:8080"
	server := &http.Server{
		Addr:    serverAddr,
		Handler: mux,
	}

	// サーバーを別ゴルーチンで起動
	go func() {
		log.Printf("[INFO] サーバーを %s で起動しました。\n", serverAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[ERROR] サーバーの起動に失敗しました: %v", err)
		}
	}()

	// グレースフルシャットダウンのためのシグナル待機
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("[INFO] 終了シグナルを受信しました。シャットダウンを開始します。")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("[ERROR] サーバーの強制停止エラー: %v", err)
	}

	log.Println("[INFO] アプリケーションが正常に終了しました。")
}
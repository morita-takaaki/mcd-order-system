package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	// ログ設定の初期化
	logDir := "logs"
	logFile := filepath.Join(logDir, "order.log")

	// logsフォルダが存在しない場合は自動作成
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatalf("ログフォルダ作成失敗: %v", err)
	}

	// 追記モードでログファイルを開く
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("ログファイルオープン失敗: %v", err)
	}
	defer f.Close()

	// 標準出力とファイル出力の両方に同時に出力する設定
	mw := io.MultiWriter(os.Stdout, f)
	log.SetOutput(mw)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	log.Println("システム起動プロセス開始...")

	// DB初期化
	initDB()
	log.Println("SQLite データベース初期化完了（order.db）")

	// ルーティング設定（外部ルーターを使用せず、標準の http.HandleFunc を使用）
	http.HandleFunc("/api/orders", handleOrders)
	http.HandleFunc("/api/kitchen", handleKitchen)
	http.HandleFunc("/api/board", handleBoard)

	address := "0.0.0.0:8080"
	log.Printf("サーバーを起動しました。ポート: %s", address)

	if err := http.ListenAndServe(address, nil); err != nil {
		log.Fatalf("サーバー起動失敗: %v", err)
	}
}
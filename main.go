package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

var Logger *log.Logger

func initLogger() {
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatalf("Failed to create log directory: %v", err)
	}

	logFile, err := os.OpenFile(filepath.Join(logDir, "order.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}

	// 標準出力とファイル出力の両方に同時に出力
	mw := io.MultiWriter(os.Stdout, logFile)
	Logger = log.New(mw, "", log.LstdFlags)
}

func main() {
	initLogger()
	InitDB()
	defer CloseDB()

	// Go 1.22+ 以降の標準マルチプレクサによるルーティング設計
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/orders", handleOrdersPost)
	mux.HandleFunc("GET /api/orders", handleOrdersGet)
	mux.HandleFunc("GET /api/orders/{orderNo}", handleOrdersGetByNo)
	mux.HandleFunc("PUT /api/orders/{orderNo}/status", handleOrdersStatusPut)

	mux.HandleFunc("POST /api/board", handleBoardPost)
	mux.HandleFunc("POST /api/kitchen", handleKitchenPost)

	// グローバルCORSミドルウェアの適用
	corsHandler := enableCORS(mux)

	Logger.Println("Server starting on 0.0.0.0:8080...")
	if err := http.ListenAndServe("0.0.0.0:8080", corsHandler); err != nil {
		Logger.Fatalf("Server failed to start: %v", err)
	}
}

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
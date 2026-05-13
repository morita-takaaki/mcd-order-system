package main

import (
	"log"
	"net/http"

	"github.com/rs/cors"
)

func main() {
	db := InitDB()
	defer db.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/orders", OrdersHandler(db))
	mux.HandleFunc("/api/orders/", OrderDetailHandler(db))

	handler := cors.AllowAll().Handler(mux)

	log.Println("サーバー起動: http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", handler))
}

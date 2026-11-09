package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const applicationVersion = "v3"

const schemaStatement = `CREATE TABLE IF NOT EXISTS items (
	id serial primary key,
	name text not null,
	created_at timestamptz default now()
)`

type itemRecord struct {
	ItemID    int64     `json:"id"`
	ItemName  string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type createItemRequest struct {
	ItemName string `json:"name"`
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type application struct {
	database *pgxpool.Pool
}

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	runContext := context.Background()
	database, databaseError := pgxpool.New(runContext, databaseURL)
	if databaseError != nil {
		log.Fatalf("connect database: %v", databaseError)
	}
	defer database.Close()
	if migrateError := migrate(runContext, database); migrateError != nil {
		log.Fatalf("migrate database: %v", migrateError)
	}
	service := &application{database: database}
	log.Printf("listening on port %s", port)
	if serveError := http.ListenAndServe(":"+port, newRouter(service)); serveError != nil {
		log.Fatalf("serve: %v", serveError)
	}
}

func migrate(runContext context.Context, database *pgxpool.Pool) error {
	_, migrateError := database.Exec(runContext, schemaStatement)
	return migrateError
}

func newRouter(service *application) http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /health", service.handleHealth)
	router.HandleFunc("GET /api/items", service.handleListItems)
	router.HandleFunc("POST /api/items", service.handleCreateItem)
	return withCORS(router)
}

func withCORS(nextHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, httpRequest *http.Request) {
		responseWriter.Header().Set("Access-Control-Allow-Origin", "*")
		responseWriter.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		responseWriter.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if httpRequest.Method == http.MethodOptions {
			responseWriter.WriteHeader(http.StatusNoContent)
			return
		}
		nextHandler.ServeHTTP(responseWriter, httpRequest)
	})
}

func (service *application) handleHealth(responseWriter http.ResponseWriter, request *http.Request) {
	writeJSON(responseWriter, http.StatusOK, healthResponse{Status: "ok", Version: applicationVersion})
}

func (service *application) handleListItems(responseWriter http.ResponseWriter, request *http.Request) {
	rows, queryError := service.database.Query(request.Context(), `SELECT id, name, created_at FROM items ORDER BY id`)
	if queryError != nil {
		writeError(responseWriter, http.StatusInternalServerError, queryError.Error())
		return
	}
	defer rows.Close()
	items := []itemRecord{}
	for rows.Next() {
		var record itemRecord
		if scanError := rows.Scan(&record.ItemID, &record.ItemName, &record.CreatedAt); scanError != nil {
			writeError(responseWriter, http.StatusInternalServerError, scanError.Error())
			return
		}
		items = append(items, record)
	}
	if rowsError := rows.Err(); rowsError != nil {
		writeError(responseWriter, http.StatusInternalServerError, rowsError.Error())
		return
	}
	writeJSON(responseWriter, http.StatusOK, items)
}

func (service *application) handleCreateItem(responseWriter http.ResponseWriter, request *http.Request) {
	var payload createItemRequest
	if decodeError := json.NewDecoder(request.Body).Decode(&payload); decodeError != nil {
		writeError(responseWriter, http.StatusBadRequest, "invalid JSON body")
		return
	}
	itemName := strings.TrimSpace(payload.ItemName)
	if itemName == "" {
		writeError(responseWriter, http.StatusBadRequest, "name is required")
		return
	}
	var record itemRecord
	scanError := service.database.QueryRow(
		request.Context(),
		`INSERT INTO items (name) VALUES ($1) RETURNING id, name, created_at`,
		itemName,
	).Scan(&record.ItemID, &record.ItemName, &record.CreatedAt)
	if scanError != nil {
		writeError(responseWriter, http.StatusInternalServerError, scanError.Error())
		return
	}
	writeJSON(responseWriter, http.StatusCreated, record)
}

func writeJSON(responseWriter http.ResponseWriter, statusCode int, payload any) {
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(statusCode)
	encoder := json.NewEncoder(responseWriter)
	encoder.SetEscapeHTML(false)
	if encodeError := encoder.Encode(payload); encodeError != nil {
		log.Printf("encode response: %v", encodeError)
	}
}

func writeError(responseWriter http.ResponseWriter, statusCode int, message string) {
	writeJSON(responseWriter, statusCode, map[string]string{"error": message})
}

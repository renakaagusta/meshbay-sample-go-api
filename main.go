package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const applicationVersion = "v4"

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
	router.HandleFunc("GET /api/items/{itemID}", service.handleGetItem)
	router.HandleFunc("PUT /api/items/{itemID}", service.handleUpdateItem)
	router.HandleFunc("DELETE /api/items/{itemID}", service.handleDeleteItem)
	return withCORS(router)
}

func withCORS(nextHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, httpRequest *http.Request) {
		responseWriter.Header().Set("Access-Control-Allow-Origin", "*")
		responseWriter.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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

func parseItemID(responseWriter http.ResponseWriter, request *http.Request) (int64, bool) {
	itemID, parseError := strconv.ParseInt(request.PathValue("itemID"), 10, 64)
	if parseError != nil || itemID <= 0 {
		writeError(responseWriter, http.StatusBadRequest, "item id must be a positive integer")
		return 0, false
	}
	return itemID, true
}

func (service *application) handleGetItem(responseWriter http.ResponseWriter, request *http.Request) {
	itemID, valid := parseItemID(responseWriter, request)
	if !valid {
		return
	}
	var record itemRecord
	scanError := service.database.QueryRow(
		request.Context(), `SELECT id, name, created_at FROM items WHERE id = $1`, itemID,
	).Scan(&record.ItemID, &record.ItemName, &record.CreatedAt)
	if errors.Is(scanError, pgx.ErrNoRows) {
		writeError(responseWriter, http.StatusNotFound, "item not found")
		return
	}
	if scanError != nil {
		writeError(responseWriter, http.StatusInternalServerError, scanError.Error())
		return
	}
	writeJSON(responseWriter, http.StatusOK, record)
}

func (service *application) handleUpdateItem(responseWriter http.ResponseWriter, request *http.Request) {
	itemID, valid := parseItemID(responseWriter, request)
	if !valid {
		return
	}
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
		`UPDATE items SET name = $2 WHERE id = $1 RETURNING id, name, created_at`,
		itemID, itemName,
	).Scan(&record.ItemID, &record.ItemName, &record.CreatedAt)
	if errors.Is(scanError, pgx.ErrNoRows) {
		writeError(responseWriter, http.StatusNotFound, "item not found")
		return
	}
	if scanError != nil {
		writeError(responseWriter, http.StatusInternalServerError, scanError.Error())
		return
	}
	writeJSON(responseWriter, http.StatusOK, record)
}

func (service *application) handleDeleteItem(responseWriter http.ResponseWriter, request *http.Request) {
	itemID, valid := parseItemID(responseWriter, request)
	if !valid {
		return
	}
	result, deleteError := service.database.Exec(request.Context(), `DELETE FROM items WHERE id = $1`, itemID)
	if deleteError != nil {
		writeError(responseWriter, http.StatusInternalServerError, deleteError.Error())
		return
	}
	if result.RowsAffected() == 0 {
		writeError(responseWriter, http.StatusNotFound, "item not found")
		return
	}
	responseWriter.WriteHeader(http.StatusNoContent)
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

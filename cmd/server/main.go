package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/JacksonOwen-41/caseflow-api/internal/database"
	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading file")
	}

	databaseURL := os.Getenv("DATABASE_URL")

	db, err := database.Connect(databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	log.Println("Connected to PostgreSQL")

	router := chi.NewRouter()

	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		stats := make(map[string]string)

		// Ping the database with automatic retries if it fails
		maxRetries := 5
		var err error
		for i := 0; i < maxRetries; i++ {
			err = db.Ping(ctx)
			if err == nil {
				break
			}
			log.Printf("Ping failed on attempt %d: %v", i+1, err)
			time.Sleep(time.Duration(i) * time.Second) // Exponential backoff
		}

		if err != nil {
			errorMessage := fmt.Sprintf("db down: %v", err)
			stats["status"] = "down"
			stats["error"] = errorMessage
			log.Printf(errorMessage)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			err = json.NewEncoder(w).Encode(stats)
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}

		// Database is up, add more statistics
		stats["status"] = "up"
		stats["message"] = "It's healthy"

		// Get database stats (like open connections, in use, idle, etc.)
		s := db.Stat()
		stats["open_connections"] = strconv.FormatInt(int64(s.TotalConns()), 10)
		stats["in_use"] = strconv.FormatInt(int64(s.AcquiredConns()), 10)
		stats["idle"] = strconv.FormatInt(int64(s.IdleConns()), 10)
		stats["wait_count"] = strconv.FormatInt(s.EmptyAcquireCount(), 10)
		stats["max_idle_closed"] = strconv.FormatInt(s.MaxIdleDestroyCount(), 10)
		stats["max_lifetime_closed"] = strconv.FormatInt(s.MaxLifetimeDestroyCount(), 10)

		// Evaluate stats to provide a health message
		if s.TotalConns() > 40 { // Assuming 50 is the max for this example
			stats["message"] = "The database is experiencing heavy load."
		}
		if s.EmptyAcquireCount() > 1000 {
			stats["message"] = "The database has a high number of wait events, indicating potential bottlenecks."
		}

		if s.MaxIdleDestroyCount() > int64(s.TotalConns())/2 {
			stats["message"] = "Many idle connections are being closed, consider revising the connection pool settings."
		}

		if s.MaxLifetimeDestroyCount() > int64(s.TotalConns())/2 {
			stats["message"] = "Many connections are being closed due to max lifetime, consider increasing max lifetime or revising the connection usage pattern."
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err = json.NewEncoder(w).Encode(stats)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	err = http.ListenAndServe(":8080", router)

	if err != nil {
		log.Fatal(err)
	}
}

package main 

import (
	
	"log"
	"net/http"
	"os"
	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"
	"github.com/JacksonOwen-41/caseflow-api/internal/database"

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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	err = http.ListenAndServe(":8080", router)

	if err != nil {
		log.Fatal(err)
	}
}
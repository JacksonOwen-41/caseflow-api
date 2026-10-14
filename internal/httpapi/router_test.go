package httpapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/JacksonOwen-41/caseflow-api/internal/database"
	"github.com/JacksonOwen-41/caseflow-api/internal/httpapi"
	"github.com/JacksonOwen-41/caseflow-api/migrations"
)

// This test uses a dedicated disposable PostgreSQL database when configured.
func TestAuthenticationFlow(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewRouter(pool)

	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	aEmail := fmt.Sprintf("alice-%s@example.test", hex.EncodeToString(nonce))
	bEmail := fmt.Sprintf("bob-%s@example.test", hex.EncodeToString(nonce))
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1 OR email = $2", aEmail, bEmail); err != nil {
			t.Errorf("clean up test users: %v", err)
		}
	})

	request := func(method, path, token string, body any, want int) map[string]any {
		t.Helper()
		var payload []byte
		if body != nil {
			var err error
			payload, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s %s: status %d, want %d; body %s", method, path, response.Code, want, response.Body.String())
		}
		if response.Body.Len() == 0 {
			return nil
		}
		var result map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	aPassword := "alice password 1234"
	bPassword := "bob password 123456"
	request(http.MethodPost, "/signup", "", map[string]string{"email": aEmail, "password": aPassword}, http.StatusCreated)
	request(http.MethodPost, "/signup", "", map[string]string{"email": bEmail, "password": bPassword}, http.StatusCreated)
	request(http.MethodPost, "/signup", "", map[string]string{"email": aEmail, "password": aPassword}, http.StatusConflict)
	request(http.MethodPost, "/login", "", map[string]string{"email": aEmail, "password": "incorrect password"}, http.StatusUnauthorized)

	aLogin := request(http.MethodPost, "/login", "", map[string]string{"email": aEmail, "password": aPassword}, http.StatusOK)
	aToken := aLogin["token"].(string)
	bLogin := request(http.MethodPost, "/login", "", map[string]string{"email": bEmail, "password": bPassword}, http.StatusOK)
	bToken := bLogin["token"].(string)
	change := map[string]string{"current_password": aPassword, "new_password": "alice new password 1234"}
	request(http.MethodPut, "/me/password", "", change, http.StatusUnauthorized)
	request(http.MethodPut, "/me/password", aToken, map[string]string{
		"current_password": aPassword, "new_password": "alice new password 1234", "user_id": bEmail,
	}, http.StatusBadRequest)
	request(http.MethodPut, "/me/password", aToken, map[string]string{
		"current_password": "wrong password", "new_password": "alice new password 1234",
	}, http.StatusUnauthorized)
	request(http.MethodPut, "/me/password", aToken, change, http.StatusNoContent)
	request(http.MethodPut, "/me/password", aToken, change, http.StatusUnauthorized)
	request(http.MethodPost, "/login", "", map[string]string{"email": aEmail, "password": aPassword}, http.StatusUnauthorized)
	newLogin := request(http.MethodPost, "/login", "", map[string]string{"email": aEmail, "password": "alice new password 1234"}, http.StatusOK)
	newToken := newLogin["token"].(string)
	request(http.MethodPost, "/logout", newToken, nil, http.StatusNoContent)
	request(http.MethodPost, "/logout", newToken, nil, http.StatusUnauthorized)
	request(http.MethodPost, "/logout", bToken, nil, http.StatusNoContent)
}
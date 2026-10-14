package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/JacksonOwen-41/caseflow-api/internal/auth"
	"github.com/JacksonOwen-41/caseflow-api/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

const sessionLifetime = 24 * time.Hour

type api struct {
	pool  *pgxpool.Pool
	store *store.Store
}

type sessionContextKey struct{}

type session struct {
	userID    string
	tokenHash []byte
}

func NewRouter(pool *pgxpool.Pool) http.Handler {
	a := &api{pool: pool, store: store.New(pool)}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/health", a.health)
	r.Group(func(r chi.Router) {
		r.Use(noStore)
		r.Post("/signup", a.signup)
		r.Post("/login", a.login)
		r.With(a.requireAuth).Post("/logout", a.logout)
		r.With(a.requireAuth).Put("/me/password", a.changePassword)
	})
	return r
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (a *api) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.pool.Ping(ctx); err != nil {
		log.Printf("database health check failed: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "down"})
		return
	}
	s := a.pool.Stat()
	writeJSON(w, http.StatusOK, map[string]string{
		"status":              "up",
		"message":             "It's healthy",
		"open_connections":    strconv.FormatInt(int64(s.TotalConns()), 10),
		"in_use":              strconv.FormatInt(int64(s.AcquiredConns()), 10),
		"idle":                strconv.FormatInt(int64(s.IdleConns()), 10),
		"max_connections":     strconv.FormatInt(int64(s.MaxConns()), 10),
		"wait_count":          strconv.FormatInt(s.EmptyAcquireCount(), 10),
		"wait_duration":       s.EmptyAcquireWaitTime().String(),
		"max_idle_closed":     strconv.FormatInt(s.MaxIdleDestroyCount(), 10),
		"max_lifetime_closed": strconv.FormatInt(s.MaxLifetimeDestroyCount(), 10),
	})
}

func (a *api) signup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	email, ok := normalizeEmail(input.Email)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}
	passwordHash, err := auth.HashPassword(input.Password)
	if errors.Is(err, auth.ErrInvalidPassword) {
		writeError(w, http.StatusBadRequest, auth.ErrInvalidPassword.Error())
		return
	}
	if err != nil {
		log.Printf("hash signup password: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	user, err := a.store.CreateUser(r.Context(), email, passwordHash)
	if store.IsUniqueViolation(err) {
		writeError(w, http.StatusConflict, "email already registered")
		return
	}
	if err != nil {
		log.Printf("create user: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (a *api) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	email, ok := normalizeEmail(input.Email)
	if !ok || input.Password == "" {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	user, err := a.store.UserByEmail(r.Context(), email)
	if store.IsNotFound(err) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		log.Printf("load user for login: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !auth.VerifyPassword(user.PasswordHash, input.Password) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	token, tokenHash, err := auth.NewToken()
	if err != nil {
		log.Printf("create session token: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	expiresAt := time.Now().UTC().Add(sessionLifetime)
	if err := a.store.CreateSession(r.Context(), user.ID, tokenHash, expiresAt); err != nil {
		log.Printf("create session: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}{Token: token, ExpiresAt: expiresAt})
}

func (a *api) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		tokenHash, ok := auth.HashToken(parts[1])
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		userID, err := a.store.UserIDForSession(r.Context(), tokenHash)
		if store.IsNotFound(err) {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if err != nil {
			log.Printf("look up session: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		ctx := context.WithValue(r.Context(), sessionContextKey{}, session{userID: userID, tokenHash: tokenHash})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	s := r.Context().Value(sessionContextKey{}).(session)
	if err := a.store.DeleteSession(r.Context(), s.tokenHash); err != nil {
		log.Printf("delete session: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if input.CurrentPassword == input.NewPassword {
		writeError(w, http.StatusBadRequest, "new password must differ from current password")
		return
	}
	s := r.Context().Value(sessionContextKey{}).(session)
	oldHash, err := a.store.PasswordHashByID(r.Context(), s.userID)
	if err != nil {
		log.Printf("load password hash: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !auth.VerifyPassword(oldHash, input.CurrentPassword) {
		writeError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	newHash, err := auth.HashPassword(input.NewPassword)
	if errors.Is(err, auth.ErrInvalidPassword) {
		writeError(w, http.StatusBadRequest, auth.ErrInvalidPassword.Error())
		return
	}
	if err != nil {
		log.Printf("hash new password: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	changed, err := a.store.ChangePassword(r.Context(), s.userID, oldHash, newHash)
	if err != nil {
		log.Printf("change password: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if !changed {
		writeError(w, http.StatusConflict, "password changed concurrently; please retry")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func normalizeEmail(input string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(input))
	if email == "" || len(email) > 250 {
		return "", false
	}
	address, err := mail.ParseAddress(email)
	return email, err == nil && address.Address == email
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("body must contain one JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

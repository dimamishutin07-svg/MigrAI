package bot

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed admin.html
var adminHTML string

//go:embed admin.js
var adminJS string

//go:embed admin.css
var adminCSS string

type loginAttempt struct {
	Count int
	Until time.Time
}
type Admin struct {
	Store                *Store
	PasswordHash, Secret string
	mu                   sync.Mutex
	attempts             map[string]loginAttempt
}

func NewAdmin(s *Store, hash, secret string) (*Admin, error) {
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != 32 || len(secret) < 32 {
		return nil, fmt.Errorf("admin credentials must be configured")
	}
	return &Admin{Store: s, PasswordHash: hash, Secret: secret, attempts: map[string]loginAttempt{}}, nil
}
func (a *Admin) signature(value string) string {
	h := hmac.New(sha256.New, []byte(a.Secret))
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}
func (a *Admin) sessionValid(r *http.Request) bool {
	c, e := r.Cookie("migrai_session")
	if e != nil {
		return false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 {
		return false
	}
	exp, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil || time.Now().Unix() >= exp {
		return false
	}
	return hmac.Equal([]byte(parts[2]), []byte(a.signature(parts[0]+"."+parts[1])))
}
func (a *Admin) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "migrai_session", Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, adminHTML)
	})
	mux.HandleFunc("GET /admin.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		fmt.Fprint(w, adminJS)
	})
	mux.HandleFunc("GET /admin.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		fmt.Fprint(w, adminCSS)
	})
	mux.HandleFunc("GET /api/users", a.protect(a.users))
	mux.HandleFunc("GET /api/events", a.protect(a.events))
	mux.HandleFunc("GET /api/fields", a.protect(func(w http.ResponseWriter, r *http.Request) {
		fields := []map[string]string{}
		for i, q := range SurveyQuestions {
			fields = append(fields, map[string]string{"key": q.Key, "label": surveyLabels[i], "section": q.Section})
		}
		writeJSON(w, fields)
	}))
	mux.HandleFunc("GET /export.xlsx", a.protect(a.export))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		origin := r.Header.Get("Origin")
		host, _, _ := net.SplitHostPort(r.Host)
		localOrigin := (host == "localhost" || host == "127.0.0.1") && origin == "http://"+r.Host
		if r.Method == "POST" && origin != "" && origin != "https://"+r.Host && !localOrigin {
			http.Error(w, "Forbidden", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func (a *Admin) protect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.sessionValid(r) {
			http.Error(w, "Требуется вход", 401)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		next(w, r.WithContext(ctx))
	}
}
func (a *Admin) login(w http.ResponseWriter, r *http.Request) {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	a.mu.Lock()
	now := time.Now()
	for key, v := range a.attempts {
		if now.After(v.Until) {
			delete(a.attempts, key)
		}
	}
	attempt := a.attempts[ip]
	if attempt.Count >= 10 {
		a.mu.Unlock()
		http.Error(w, "Слишком много попыток. Повторите через 5 минут.", 429)
		return
	}
	attempt.Count++
	attempt.Until = now.Add(5 * time.Minute)
	a.attempts[ip] = attempt
	a.mu.Unlock()
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Некорректный запрос", 400)
		return
	}
	hash := sha256.Sum256([]byte(r.FormValue("password")))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(hash[:])), []byte(a.PasswordHash)) != 1 {
		http.Error(w, "Неверный пароль", 401)
		return
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		http.Error(w, "Ошибка входа", 500)
		return
	}
	value := strconv.FormatInt(time.Now().Add(8*time.Hour).Unix(), 10) + "." + hex.EncodeToString(nonce)
	http.SetCookie(w, &http.Cookie{Name: "migrai_session", Value: value + "." + a.signature(value), Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: 28800})
	a.mu.Lock()
	delete(a.attempts, ip)
	a.mu.Unlock()
	w.WriteHeader(204)
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func pageOffset(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if n < 0 {
		return 0
	}
	return min(n, 10000000)
}
func (a *Admin) users(w http.ResponseWriter, r *http.Request) {
	items, total, err := a.Store.AdminUsers(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("stage"), pageOffset(r), 50)
	if err != nil {
		http.Error(w, "Не удалось загрузить пользователей", 500)
		return
	}
	writeJSON(w, map[string]any{"items": items, "total": total})
}
func (a *Admin) events(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.AdminEvents(r.Context(), r.URL.Query().Get("id"), pageOffset(r))
	if err != nil {
		http.Error(w, "Не удалось загрузить историю", 400)
		return
	}
	writeJSON(w, items)
}
func StartAdmin(s *Store) error {
	if os.Getenv("ADMIN_PASSWORD_SHA256") == "" {
		return nil
	}
	a, err := NewAdmin(s, os.Getenv("ADMIN_PASSWORD_SHA256"), os.Getenv("ADMIN_SESSION_KEY"))
	if err != nil {
		return err
	}
	server := &http.Server{Addr: ":8080", Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	return server.ListenAndServe()
}

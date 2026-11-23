package bot

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAdminAuthorization(t *testing.T) {
	hash := sha256.Sum256([]byte("test-password"))
	a, err := NewAdmin(nil, fmt.Sprintf("%x", hash), strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	h := a.Handler()
	for _, path := range []string{"/api/users", "/api/events?id=1", "/export.xlsx", "/api/fields"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatalf("%s status %d", path, w.Code)
		}
	}
	login := func(password, origin string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "https://panel.test/login", strings.NewReader(url.Values{"password": {password}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		h.ServeHTTP(w, r)
		return w
	}
	if login("wrong", "").Code != 401 {
		t.Fatal("bad password accepted")
	}
	if login("test-password", "https://other.test").Code != 403 {
		t.Fatal("cross-site login accepted")
	}
	w := login("test-password", "https://panel.test")
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session")
	}
	r := httptest.NewRequest("GET", "/api/fields", nil)
	r.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("valid session refused")
	}
	cookies[0].Value += "x"
	r = httptest.NewRequest("GET", "/api/fields", nil)
	r.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("tampered session accepted")
	}
}
func TestExportPreservesUntrustedText(t *testing.T) {
	data, err := exportXLSX([][]string{{"ID", "Портрет"}, {"9007199254740993", "=HYPERLINK(\"https://example.org\") <&>"}})
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name != "xl/worksheets/sheet1.xml" {
			continue
		}
		r, _ := f.Open()
		body, _ := io.ReadAll(r)
		r.Close()
		s := string(body)
		if strings.Contains(s, "<f>") || !strings.Contains(s, "&lt;&amp;&gt;") || !strings.Contains(s, "9007199254740993") {
			t.Fatal("unsafe export")
		}
	}
}

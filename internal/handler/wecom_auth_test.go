package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func wecomTestHandler(t *testing.T) *WeComAuthHandler {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&types.WeComFlow{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{WeComAuth: &config.WeComAuthConfig{Enable: true, CorpID: "corp", AgentID: "10001", PublicOrigin: "https://example.com"}}
	return NewWeComAuthHandler(service.NewWeComService(cfg, repository.NewWeComRepository(db), nil), nil)
}
func TestWeComHandlerBrowserBoundStart(t *testing.T) {
	h := wecomTestHandler(t)
	r := gin.New()
	r.GET("/start", h.Start)
	req := httptest.NewRequest("GET", "/start?redirect=https://attacker.example", nil)
	req.Host = "attacker.example"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatal(w.Code)
	}
	if strings.Contains(w.Header().Get("Location"), "attacker") {
		t.Fatal("host/open redirect trusted")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing binding cookie")
	}
	c := cookies[0]
	if c.Name != wecomBrowserCookie || !c.HttpOnly || !c.Secure || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe cookie: %+v", c)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("missing security headers")
	}
}
func TestWeComHandlerRejectsOriginAndMissingCookies(t *testing.T) {
	h := wecomTestHandler(t)
	r := gin.New()
	r.POST("/exchange", h.Exchange)
	for _, origin := range []string{"", "https://attacker.example", "https://example.com"} {
		req := httptest.NewRequest("POST", "/exchange", strings.NewReader(`{"flow_id":"019c6e27-e55b-73d1-87d8-4e01f1f75043"}`))
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := http.StatusForbidden
		if origin == "https://example.com" {
			want = http.StatusUnauthorized
		}
		if w.Code != want {
			t.Fatalf("origin=%s status=%d body=%s", origin, w.Code, w.Body.String())
		}
	}
}
func TestWeComDisabledAndLite(t *testing.T) {
	h := wecomTestHandler(t)
	old := Edition
	t.Cleanup(func() { Edition = old })
	for _, lite := range []bool{false, true} {
		h.service.Config.Enable = lite
		Edition = "standard"
		if lite {
			Edition = "lite"
		}
		r := gin.New()
		r.GET("/config", h.Config)
		r.GET("/start", h.Start)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/config", nil))
		if strings.TrimSpace(w.Body.String()) != `{"enabled":false}` {
			t.Fatal(w.Body.String())
		}
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/start", nil))
		if w.Code != http.StatusNotFound {
			t.Fatal(w.Code)
		}
	}
}

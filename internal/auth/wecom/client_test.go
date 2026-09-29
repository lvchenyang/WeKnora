package wecom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
)

func TestCorporateLoginProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, identity string
		status         int
		wantOK         bool
	}{
		{"active", `{"userid":"Alice"}`, 1, true},
		{"visitor", `{"openid":"visitor"}`, 1, false},
		{"external", `{"external_userid":"wm123"}`, 1, false},
		{"disabled", `{"userid":"Alice"}`, 2, false},
		{"not activated", `{"userid":"Alice"}`, 4, false},
		{"left", `{"userid":"Alice"}`, 5, false},
		{"interconnected", `{"userid":"othercorp/Alice"}`, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := map[string]int{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls[r.URL.Path]++
				switch r.URL.Path {
				case "/cgi-bin/gettoken":
					if r.URL.Query().Get("corpid") != "corp" || r.URL.Query().Get("corpsecret") != "secret" {
						t.Error("incorrect app credentials")
					}
					w.Write([]byte(`{"access_token":"cached-token","expires_in":7200}`))
				case "/cgi-bin/auth/getuserinfo":
					if r.URL.Query().Get("code") != "one-use-code" {
						t.Error("incorrect OAuth code")
					}
					w.Write([]byte(tc.identity))
				case "/cgi-bin/agent/get":
					w.Write([]byte(`{"agentid":10001,"close":0}`))
				case "/cgi-bin/user/get":
					json.NewEncoder(w).Encode(Member{UserID: "ALICE", Name: "Alice", Status: tc.status})
				default:
					t.Error("unexpected endpoint")
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			client := NewClient(config.WeComAuthConfig{CorpID: "corp", AgentID: "10001", Secret: "secret"})
			client.baseURL = server.URL
			member, err := client.ResolveCode(context.Background(), "one-use-code")
			if (err == nil) != tc.wantOK {
				t.Fatalf("member=%v err=%v", member, err)
			}
			if tc.wantOK && (member.UserID != "alice" || calls["/cgi-bin/gettoken"] != 1) {
				t.Fatal("normalization or token cache failed")
			}
		})
	}
}

func TestInvalidTokenRetryAndSanitizedFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      int
		status    int
		wantCalls int
	}{{"expired", 42001, 200, 2}, {"invalid", 40014, 200, 2}, {"denied", 48002, 200, 1}, {"unavailable", 0, 503, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			codeCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/cgi-bin/gettoken" {
					w.Write([]byte(`{"access_token":"secret-token","expires_in":7200}`))
					return
				}
				codeCalls++
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(map[string]any{"errcode": tc.code, "errmsg": "sensitive-secret"})
			}))
			defer server.Close()
			c := NewClient(config.WeComAuthConfig{})
			c.baseURL = server.URL
			_, err := c.ResolveCode(context.Background(), "secret-code")
			if err == nil || codeCalls != tc.wantCalls {
				t.Fatalf("err=%v calls=%d", err, codeCalls)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), server.URL) {
				t.Fatal("credential leaked")
			}
		})
	}
}

package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWeComConfig(t *testing.T) {
	if err := (*WeComAuthConfig)(nil).Validate(); err != nil {
		t.Fatal(err)
	}
	c := WeComAuthConfig{Enable: true, CorpID: "ww123", AgentID: "10001", Secret: "private-value", PublicOrigin: "https://knowledge.example.com"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com/", "https://example.com/a", "https://example.com?next=other", "https://example.com#fragment", "//example.com"} {
		c.PublicOrigin = origin
		if c.Validate() == nil {
			t.Errorf("accepted unsafe origin %s", origin)
		}
	}
	data, _ := json.Marshal(c)
	if strings.Contains(string(data), "private-value") {
		t.Fatal("secret serialized")
	}
	t.Setenv("WECOM_AUTH_ENABLED", "true")
	t.Setenv("WECOM_AUTH_CORP_ID", "ww-env")
	t.Setenv("WECOM_AUTH_SECRET", "env-secret")
	var cfg Config
	applyWeComEnvOverrides(&cfg)
	if !cfg.WeComAuth.Enable || cfg.WeComAuth.CorpID != "ww-env" || cfg.WeComAuth.Secret != "env-secret" {
		t.Fatal("env not applied")
	}
}

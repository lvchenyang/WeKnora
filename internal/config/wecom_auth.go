package config

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// WeComAuthConfig configures one self-built corporate application per server.
// Secrets are environment-only and excluded from JSON configuration responses.
type WeComAuthConfig struct {
	Enable       bool   `yaml:"enable" json:"enable"`
	CorpID       string `yaml:"corp_id" json:"corp_id"`
	AgentID      string `yaml:"agent_id" json:"agent_id"`
	Secret       string `yaml:"-" json:"-"`
	PublicOrigin string `yaml:"public_origin" json:"public_origin"`
}

func applyWeComEnvOverrides(cfg *Config) {
	if cfg.WeComAuth == nil {
		cfg.WeComAuth = &WeComAuthConfig{}
	}
	c := cfg.WeComAuth
	if v, ok := os.LookupEnv("WECOM_AUTH_ENABLED"); ok {
		c.Enable = strings.EqualFold(v, "true")
	}
	for key, dst := range map[string]*string{"WECOM_AUTH_CORP_ID": &c.CorpID, "WECOM_AUTH_AGENT_ID": &c.AgentID, "WECOM_AUTH_PUBLIC_ORIGIN": &c.PublicOrigin} {
		if v, ok := os.LookupEnv(key); ok {
			*dst = strings.TrimSpace(v)
		}
	}
	c.Secret = strings.TrimSpace(os.Getenv("WECOM_AUTH_SECRET"))
}

func (c *WeComAuthConfig) Validate() error {
	if c == nil || !c.Enable {
		return nil
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(c.CorpID) || !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(c.AgentID) || c.Secret == "" {
		return fmt.Errorf("enabled wecom_auth requires corp_id, numeric agent_id and WECOM_AUTH_SECRET")
	}
	u, err := url.Parse(c.PublicOrigin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return fmt.Errorf("wecom_auth.public_origin must be an HTTPS origin without path, query or fragment")
	}
	return nil
}

// Package wecom implements the corporate self-built application login protocol.
package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
)

var ErrUnavailable = errors.New("WeCom service unavailable")
var ErrNotMember = errors.New("active application member required")

// Provider is deliberately small so protocol failures can be tested without
// redirecting real credentials to an alternative production API host.
type Provider interface {
	ResolveCode(context.Context, string) (*Member, error)
	Member(context.Context, string) (*Member, error)
}

type Member struct {
	UserID string `json:"userid"`
	Name   string `json:"name"`
	Status int    `json:"status"`
}

type Client struct {
	cfg     config.WeComAuthConfig
	http    *http.Client
	baseURL string
	mu      sync.Mutex
	token   string
	expires time.Time
}

func NewClient(cfg config.WeComAuthConfig) *Client {
	return &Client{cfg: cfg, baseURL: "https://qyapi.weixin.qq.com", http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

var subjectPattern = regexp.MustCompile(`^[a-zA-Z0-9_.@=-]{1,64}$`)

func NormalizeSubject(value string) (string, error) {
	if !subjectPattern.MatchString(value) {
		return "", ErrNotMember
	}
	return strings.ToLower(value), nil
}

// get intentionally never returns transport errors or provider errmsg: those
// may contain the complete URL (including secret/access_token/code).
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return 0, ErrUnavailable
	}
	res, err := c.http.Do(req)
	if err != nil {
		return 0, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return 0, ErrUnavailable
	}
	var status struct {
		ErrCode int `json:"errcode"`
	}
	if json.Unmarshal(data, &status) != nil {
		return 0, ErrUnavailable
	}
	if status.ErrCode != 0 {
		return status.ErrCode, fmt.Errorf("WeCom rejected request (%d)", status.ErrCode)
	}
	if json.Unmarshal(data, out) != nil {
		return 0, ErrUnavailable
	}
	return 0, nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	_, err := c.get(ctx, "/cgi-bin/gettoken", url.Values{"corpid": {c.cfg.CorpID}, "corpsecret": {c.cfg.Secret}}, &out)
	if err != nil {
		return "", err
	}
	if out.AccessToken == "" || out.ExpiresIn <= 0 {
		return "", ErrUnavailable
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl > 60*time.Second {
		ttl -= 60 * time.Second
	}
	c.token, c.expires = out.AccessToken, time.Now().Add(ttl)
	return c.token, nil
}

func (c *Client) api(ctx context.Context, path string, q url.Values, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.accessToken(ctx)
		if err != nil {
			return err
		}
		q.Set("access_token", token)
		code, err := c.get(ctx, path, q, out)
		if err == nil {
			return nil
		}
		// Retry only explicit expired/invalid-token errors; a timeout may have
		// already consumed an OAuth code and must not be blindly replayed.
		if attempt == 0 && (code == 40014 || code == 42001) {
			c.mu.Lock()
			if c.token == token {
				c.token = ""
			}
			c.mu.Unlock()
			continue
		}
		return err
	}
	return ErrUnavailable
}

func (c *Client) ResolveCode(ctx context.Context, code string) (*Member, error) {
	if len(code) == 0 || len(code) > 512 {
		return nil, ErrNotMember
	}
	var out struct {
		UserID         string `json:"userid"`
		OpenID         string `json:"openid"`
		ExternalUserID string `json:"external_userid"`
	}
	if err := c.api(ctx, "/cgi-bin/auth/getuserinfo", url.Values{"code": {code}}, &out); err != nil {
		return nil, err
	}
	if out.UserID == "" || out.OpenID != "" || out.ExternalUserID != "" {
		return nil, ErrNotMember
	}
	return c.Member(ctx, out.UserID)
}

func (c *Client) Member(ctx context.Context, subject string) (*Member, error) {
	normalized, err := NormalizeSubject(subject)
	if err != nil {
		return nil, err
	}
	var app struct {
		AgentID json.Number `json:"agentid"`
		Close   int         `json:"close"`
	}
	if err := c.api(ctx, "/cgi-bin/agent/get", url.Values{"agentid": {c.cfg.AgentID}}, &app); err != nil {
		return nil, err
	}
	if app.AgentID.String() != c.cfg.AgentID || app.Close != 0 {
		return nil, ErrNotMember
	}
	var member Member
	if err := c.api(ctx, "/cgi-bin/user/get", url.Values{"userid": {subject}}, &member); err != nil {
		return nil, err
	}
	got, err := NormalizeSubject(member.UserID)
	if err != nil || got != normalized || member.Status != 1 {
		return nil, ErrNotMember
	}
	member.UserID = got
	return &member, nil
}

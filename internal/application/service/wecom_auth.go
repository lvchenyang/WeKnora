package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/auth/wecom"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

type WeComService struct {
	Config     *config.WeComAuthConfig
	Repository *repository.WeComRepository
	users      interfaces.UserService
	provider   wecom.Provider
}

func NewWeComService(cfg *config.Config, repo *repository.WeComRepository, users interfaces.UserService) *WeComService {
	c := cfg.WeComAuth
	if c == nil {
		c = &config.WeComAuthConfig{}
	}
	return &WeComService{Config: c, Repository: repo, users: users, provider: wecom.NewClient(*c)}
}

func WeComSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func WeComDigest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func (s *WeComService) Start(ctx context.Context, browser string, inApp bool) (string, error) {
	state, err := WeComSecret()
	if err != nil {
		return "", err
	}
	flow := &types.WeComFlow{ID: uuid.NewString(), Purpose: "state", SecretHash: WeComDigest(state), BrowserHash: WeComDigest(browser), ExpiresAt: time.Now().Add(5 * time.Minute)}
	if err := s.Repository.CreateFlow(ctx, flow); err != nil {
		return "", err
	}
	callback := s.Config.PublicOrigin + "/api/v1/auth/wecom/callback"
	if inApp {
		q := url.Values{"appid": {s.Config.CorpID}, "agentid": {s.Config.AgentID}, "redirect_uri": {callback}, "response_type": {"code"}, "scope": {"snsapi_base"}, "state": {state}}
		return "https://open.weixin.qq.com/connect/oauth2/authorize?" + q.Encode() + "#wechat_redirect", nil
	}
	q := url.Values{"login_type": {"CorpApp"}, "appid": {s.Config.CorpID}, "agentid": {s.Config.AgentID}, "redirect_uri": {callback}, "state": {state}, "lang": {"zh"}}
	return "https://login.work.weixin.qq.com/wwlogin/sso/login?" + q.Encode(), nil
}

func (s *WeComService) Callback(ctx context.Context, code, state, browser string) (string, string, error) {
	if len(state) != 64 || len(browser) != 64 {
		return "", "", repository.ErrTokenNotFound
	}
	if err := s.Repository.ConsumeState(ctx, WeComDigest(state), WeComDigest(browser)); err != nil {
		return "", "", err
	}
	member, err := s.provider.ResolveCode(ctx, code)
	if err != nil {
		return "", "", err
	}
	identity, err := s.Repository.FindIdentity(ctx, s.Config.CorpID, member.UserID)
	if err != nil {
		return "", "", err
	}
	ticket, err := WeComSecret()
	if err != nil {
		return "", "", err
	}
	flow := &types.WeComFlow{ID: uuid.NewString(), Purpose: "ticket", SecretHash: WeComDigest(ticket), BrowserHash: WeComDigest(browser), UserID: identity.UserID, IdentityID: identity.ID, IdentityVersion: identity.Version, ExpiresAt: time.Now().Add(time.Minute)}
	if err := s.Repository.CreateFlow(ctx, flow); err != nil {
		return "", "", err
	}
	return flow.ID, ticket, nil
}

func (s *WeComService) Exchange(ctx context.Context, id, ticket, browser string) (*types.LoginResponse, error) {
	if _, err := uuid.Parse(id); err != nil || len(ticket) != 64 || len(browser) != 64 {
		return nil, repository.ErrTokenNotFound
	}
	grant, err := s.Repository.Grant(ctx, id, WeComDigest(ticket), WeComDigest(browser))
	if err != nil {
		return nil, err
	}
	return s.users.LoginWithWeCom(ctx, grant)
}

type WeComBindingPreview struct {
	PreviewToken string `json:"preview_token"`
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	Username     string `json:"username"`
	CorpID       string `json:"corp_id"`
	Subject      string `json:"subject"`
	DisplayName  string `json:"display_name"`
}

func (s *WeComService) Preview(ctx context.Context, actor, email, subject string) (*WeComBindingPreview, error) {
	user, err := s.users.GetUserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		return nil, repository.ErrTokenNotFound
	}
	member, err := s.provider.Member(ctx, subject)
	if err != nil {
		return nil, err
	}
	token, err := WeComSecret()
	if err != nil {
		return nil, err
	}
	flow := &types.WeComFlow{ID: uuid.NewString(), Purpose: "bind", CorpID: s.Config.CorpID, SecretHash: WeComDigest(token), UserID: user.ID, Subject: member.UserID, DisplayName: member.Name, ActorID: actor, ExpiresAt: time.Now().Add(5 * time.Minute)}
	if err := s.Repository.CreateFlow(ctx, flow); err != nil {
		return nil, err
	}
	return &WeComBindingPreview{PreviewToken: token, UserID: user.ID, Email: user.Email, Username: user.Username, CorpID: s.Config.CorpID, Subject: member.UserID, DisplayName: member.Name}, nil
}

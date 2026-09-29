package service

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/auth/wecom"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type testWeComProvider struct {
	member *wecom.Member
	err    error
	calls  int
}

func (p *testWeComProvider) ResolveCode(context.Context, string) (*wecom.Member, error) {
	p.calls++
	return p.member, p.err
}
func (p *testWeComProvider) Member(context.Context, string) (*wecom.Member, error) {
	return p.member, p.err
}

func TestWeComExistingAccountLoginLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wecom.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	if err := db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.AuthToken{}, &types.WeComIdentity{}, &types.WeComFlow{}, &types.WeComIdentityEvent{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{WeComAuth: &config.WeComAuthConfig{Enable: true, CorpID: "corp", AgentID: "10001", Secret: "secret", PublicOrigin: "https://knowledge.example.com"}}
	userRepo := repository.NewUserRepository(db)
	tokens := repository.NewAuthTokenRepository(db, cfg)
	bindings := repository.NewWeComRepository(db)
	user := &types.User{ID: uuid.NewString(), Email: "existing@example.com", Username: "existing", PasswordHash: "unchanged", IsActive: true, IsSystemAdmin: true}
	if err := userRepo.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	users := &userService{config: cfg, userRepo: userRepo, tokenRepo: tokens, tenantService: &provisioningTenantService{}}
	provider := &testWeComProvider{member: &wecom.Member{UserID: "employee", Name: "Employee", Status: 1}}
	login := &WeComService{Config: cfg.WeComAuth, Repository: bindings, users: users, provider: provider}
	preview, err := login.Preview(ctx, user.ID, user.Email, "employee")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := bindings.Bind(ctx, "corp", WeComDigest(preview.PreviewToken), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	browser, _ := WeComSecret()
	start, err := login.Start(ctx, browser, false)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(start)
	if target.Host != "login.work.weixin.qq.com" || target.Query().Get("login_type") != "CorpApp" || target.Query().Get("redirect_uri") != cfg.WeComAuth.PublicOrigin+"/api/v1/auth/wecom/callback" {
		t.Fatal("invalid desktop authorization URL")
	}
	state := target.Query().Get("state")
	if _, _, err := login.Callback(ctx, "code", state, strings.Repeat("0", 64)); err == nil {
		t.Fatal("foreign browser accepted")
	}
	if provider.calls != 0 {
		t.Fatal("code consumed before browser verification")
	}
	flow, ticket, err := login.Callback(ctx, "code", state, browser)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := login.Callback(ctx, "code", state, browser); err == nil {
		t.Fatal("state replay accepted")
	}
	response, err := login.Exchange(ctx, flow, ticket, browser)
	if err != nil {
		t.Fatal(err)
	}
	if response.User.ID != user.ID || response.User.Email != user.Email || response.ActiveTenant != nil {
		t.Fatal("existing tenantless account replaced/provisioned")
	}
	var count int64
	db.Model(&types.User{}).Count(&count)
	if count != 1 {
		t.Fatal("new account created")
	}
	if _, err := login.Exchange(ctx, flow, ticket, browser); err == nil {
		t.Fatal("ticket replay accepted")
	}
	validated, tenant, err := users.ValidateToken(ctx, response.Token)
	if err != nil || validated.ID != user.ID || tenant != 0 {
		t.Fatalf("validate: %v", err)
	}
	original, _ := tokens.GetTokenByValue(ctx, response.Token)
	access, refresh, err := users.RefreshToken(ctx, response.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, _ := tokens.GetTokenByValue(ctx, access)
	if refreshed.AuthMethod != "wecom" || refreshed.ExternalIdentityID != identity.ID || refreshed.SessionFamilyID != original.SessionFamilyID {
		t.Fatal("refresh lost provenance")
	}
	if _, _, err := users.RefreshToken(ctx, response.RefreshToken); err == nil {
		t.Fatal("refresh replay accepted")
	}
	// Give the same account a second workspace and exercise the actual switch
	// service with the authenticated access token carried by the middleware.
	users.memberService = &membershipLookupService{byTenant: map[uint64]*types.TenantMember{42: {TenantID: 42, UserID: user.ID, Status: types.TenantMemberStatusActive}}}
	switched, err := users.SwitchTenant(context.WithValue(ctx, types.AuthSourceContextKey, access), user, 42, refresh)
	if err != nil {
		t.Fatal(err)
	}
	switchRow, _ := tokens.GetTokenByValue(ctx, switched.Token)
	if switchRow.ExternalIdentityID != identity.ID || switchRow.SessionFamilyID != original.SessionFamilyID {
		t.Fatal("workspace switch lost provenance")
	}
	if _, err := users.SwitchTenant(context.WithValue(ctx, types.AuthSourceContextKey, switched.Token), user, 999, switched.RefreshToken); err == nil {
		t.Fatal("workspace membership bypass")
	}
	if err := bindings.SetStatus(ctx, "corp", identity.ID, user.ID, "suspended", identity.Version); err != nil {
		t.Fatal(err)
	}
	if _, _, err := users.ValidateToken(ctx, switched.Token); err == nil {
		t.Fatal("suspended access accepted")
	}
	if _, _, err := users.RefreshToken(ctx, switched.RefreshToken); err == nil {
		t.Fatal("suspended refresh accepted")
	}
	if _, err := users.GetAccessTokenByID(ctx, switchRow.ID); !errors.Is(err, repository.ErrTokenNotFound) {
		t.Fatalf("terminal recheck should reject definitively: %v", err)
	}
}

func TestWeComInAppAndUnboundAccount(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wecom.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.AutoMigrate(&types.WeComFlow{}, &types.WeComIdentity{}); err != nil {
		t.Fatal(err)
	}
	provider := &testWeComProvider{member: &wecom.Member{UserID: "unknown", Status: 1}}
	service := &WeComService{Config: &config.WeComAuthConfig{CorpID: "corp", AgentID: "10001", PublicOrigin: "https://example.com"}, Repository: repository.NewWeComRepository(db), provider: provider}
	browser, _ := WeComSecret()
	target, err := service.Start(context.Background(), browser, true)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(target)
	if u.Host != "open.weixin.qq.com" || u.Query().Get("scope") != "snsapi_base" || u.Fragment != "wechat_redirect" {
		t.Fatal("wrong in-app protocol")
	}
	if _, _, err := service.Callback(context.Background(), "code", u.Query().Get("state"), browser); err == nil {
		t.Fatal("unbound member accepted")
	}
	var count int64
	db.Model(&types.WeComFlow{}).Where("purpose = ?", "ticket").Count(&count)
	if count != 0 {
		t.Fatal("ticket for unbound account")
	}
}

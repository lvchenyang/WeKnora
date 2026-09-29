package service

import (
	"context"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

func (s *userService) validateSessionIdentity(ctx context.Context, token *types.AuthToken) error {
	if token == nil {
		return apprepo.ErrTokenNotFound
	}
	if token.AuthMethod == "wecom" && (s.config == nil || s.config.WeComAuth == nil || !s.config.WeComAuth.Enable) {
		return apprepo.ErrTokenNotFound
	}
	return s.tokenRepo.ValidateTokenIdentity(ctx, token)
}

// LoginWithWeCom preserves the existing local account, tenant landing policy,
// memberships and web_user principal. No registration/email matching happens.
func (s *userService) LoginWithWeCom(ctx context.Context, grant *types.WeComLoginGrant) (*types.LoginResponse, error) {
	if grant == nil || s.config == nil || s.config.WeComAuth == nil || !s.config.WeComAuth.Enable {
		return nil, apprepo.ErrTokenNotFound
	}
	user, err := s.userRepo.GetUserByID(ctx, grant.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		return nil, apprepo.ErrTokenNotFound
	}
	tenantID := s.resolveLoginTenantID(ctx, user)
	var tenant *types.Tenant
	if tenantID != 0 {
		tenant, err = s.tenantService.GetTenantByID(ctx, tenantID)
		if err != nil {
			return nil, err
		}
	}
	access, refresh, err := s.issueTokensForTenant(ctx, user, tenantID, types.SessionIssue{Grant: grant})
	if err != nil {
		return nil, err
	}
	return &types.LoginResponse{Success: true, User: user, ActiveTenant: tenant, Memberships: s.buildMembershipsForUser(ctx, user, tenant), Token: access, RefreshToken: refresh}, nil
}

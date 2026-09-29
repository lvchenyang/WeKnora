package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// lockAuthUser takes a write lock before reading session/identity state. The
// no-op UPDATE works on both PostgreSQL and SQLite (which has no FOR UPDATE).
// Every token-pair issuer and identity mutation uses this ordering.
func lockAuthUser(tx *gorm.DB, userID string) error {
	r := tx.Model(&types.User{}).Where("id = ? AND is_active = ?", userID, true).UpdateColumn("id", gorm.Expr("id"))
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrTokenNotFound
	}
	return nil
}

func validateTokenIdentity(db *gorm.DB, token *types.AuthToken, corp string) error {
	if token == nil || token.IsRevoked {
		return ErrTokenNotFound
	}
	if token.AuthMethod != "wecom" {
		if token.ExternalIdentityID != "" {
			return ErrTokenNotFound
		}
		return nil
	}
	if token.ExternalIdentityID == "" {
		return ErrTokenNotFound
	}
	var n int64
	if err := db.Model(&types.WeComIdentity{}).Where("id = ? AND user_id = ? AND status = ? AND version = ? AND corp_id = ?", token.ExternalIdentityID, token.UserID, "active", token.ExternalIdentityVersion, corp).Count(&n).Error; err != nil {
		return err
	}
	if n != 1 {
		return ErrTokenNotFound
	}
	return nil
}

func (r *authTokenRepository) ValidateTokenIdentity(ctx context.Context, token *types.AuthToken) error {
	return validateTokenIdentity(r.db.WithContext(ctx), token, r.wecomCorpID)
}

func consumeWeComFlow(tx *gorm.DB, id, purpose, secretHash, browserHash string) error {
	result := tx.Where("id = ? AND purpose = ? AND secret_hash = ? AND browser_hash = ? AND expires_at > ?", id, purpose, secretHash, browserHash, time.Now()).Delete(&types.WeComFlow{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrTokenNotFound
	}
	return nil
}

// CreateTokenPair persists both credentials, preserves provenance, and consumes
// the old refresh token or browser-bound grant in one transaction. A failed
// write cannot leave a usable half-session or spend a grant without a session.
func (r *authTokenRepository) CreateTokenPair(ctx context.Context, access, refresh *types.AuthToken, issue types.SessionIssue) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if access.UserID != refresh.UserID {
			return ErrTokenNotFound
		}
		if err := lockAuthUser(tx, access.UserID); err != nil {
			return err
		}
		if issue.Source != nil {
			var source types.AuthToken
			if err := tx.First(&source, "id = ?", issue.Source.ID).Error; err != nil {
				return err
			}
			if source.UserID != access.UserID || !source.ExpiresAt.After(time.Now()) {
				return ErrTokenNotFound
			}
			if err := validateTokenIdentity(tx, &source, r.wecomCorpID); err != nil {
				return err
			}
			if access.AuthMethod != source.AuthMethod || access.ExternalIdentityID != source.ExternalIdentityID || access.ExternalIdentityVersion != source.ExternalIdentityVersion || (source.SessionFamilyID != "" && access.SessionFamilyID != source.SessionFamilyID) {
				return ErrTokenNotFound
			}
		}
		if issue.RefreshToken != "" {
			var old types.AuthToken
			if err := tx.Where("token = ?", issue.RefreshToken).First(&old).Error; err != nil {
				return err
			}
			if old.UserID != access.UserID || old.TokenType != "refresh_token" || !old.ExpiresAt.After(time.Now()) {
				return ErrTokenNotFound
			}
			if err := validateTokenIdentity(tx, &old, r.wecomCorpID); err != nil {
				return err
			}
			// Legacy rows have no family; only accept another legacy credential of
			// the same account. New sessions must match the authenticating family.
			if issue.Source == nil || (issue.Source.SessionFamilyID != "" && old.SessionFamilyID != issue.Source.SessionFamilyID) || (issue.Source.SessionFamilyID == "" && old.SessionFamilyID != "") {
				return ErrTokenNotFound
			}
			result := tx.Model(&types.AuthToken{}).Where("id = ? AND is_revoked = ?", old.ID, false).Update("is_revoked", true)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrTokenNotFound
			}
		}
		if grant := issue.Grant; grant != nil {
			var flow types.WeComFlow
			if err := tx.First(&flow, "id = ?", grant.FlowID).Error; err != nil {
				return err
			}
			if flow.UserID != access.UserID || flow.IdentityID != access.ExternalIdentityID || flow.IdentityVersion != access.ExternalIdentityVersion || access.AuthMethod != "wecom" {
				return ErrTokenNotFound
			}
			if err := consumeWeComFlow(tx, grant.FlowID, "ticket", grant.SecretHash, grant.BrowserHash); err != nil {
				return err
			}
		} else if access.AuthMethod == "wecom" && issue.Source == nil {
			return ErrTokenNotFound
		}
		if err := validateTokenIdentity(tx, access, r.wecomCorpID); err != nil {
			return err
		}
		if err := tx.Create(access).Error; err != nil {
			return err
		}
		return tx.Create(refresh).Error
	})
}

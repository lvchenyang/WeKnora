package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrWeComConflict = errors.New("WeCom binding changed or already exists")

type WeComRepository struct{ db *gorm.DB }

func NewWeComRepository(db *gorm.DB) *WeComRepository { return &WeComRepository{db: db} }

func (r *WeComRepository) CreateFlow(ctx context.Context, flow *types.WeComFlow) error {
	// Keep the short-lived store bounded during normal operation. No secrets or
	// OAuth authorization codes are retained in either the flow or audit tables.
	if err := r.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&types.WeComFlow{}).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(flow).Error
}

func (r *WeComRepository) ConsumeState(ctx context.Context, secretHash, browserHash string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("purpose = ? AND secret_hash = ? AND browser_hash = ? AND expires_at > ?", "state", secretHash, browserHash, time.Now()).Delete(&types.WeComFlow{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTokenNotFound
		}
		return nil
	})
}

func (r *WeComRepository) Grant(ctx context.Context, id, secretHash, browserHash string) (*types.WeComLoginGrant, error) {
	var f types.WeComFlow
	if err := r.db.WithContext(ctx).Where("id = ? AND purpose = ? AND secret_hash = ? AND browser_hash = ? AND expires_at > ?", id, "ticket", secretHash, browserHash, time.Now()).First(&f).Error; err != nil {
		return nil, err
	}
	return &types.WeComLoginGrant{FlowID: f.ID, SecretHash: secretHash, BrowserHash: browserHash, UserID: f.UserID, IdentityID: f.IdentityID, IdentityVersion: f.IdentityVersion}, nil
}

func (r *WeComRepository) FindIdentity(ctx context.Context, corp, subject string) (*types.WeComIdentity, error) {
	var identity types.WeComIdentity
	if err := r.db.WithContext(ctx).Where("corp_id = ? AND subject = ? AND status = ?", corp, subject, "active").First(&identity).Error; err != nil {
		return nil, err
	}
	return &identity, nil
}

func (r *WeComRepository) List(ctx context.Context, corp string, offset int) ([]types.WeComIdentity, error) {
	var rows []types.WeComIdentity
	err := r.db.WithContext(ctx).Where("corp_id = ?", corp).Order("created_at DESC, id DESC").Offset(offset).Limit(100).Find(&rows).Error
	return rows, err
}

func assertWeComAdmin(tx *gorm.DB, actorID string) error {
	var count int64
	if err := tx.Model(&types.User{}).Where("id = ? AND is_active = ? AND is_system_admin = ?", actorID, true, true).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrTokenNotFound
	}
	return nil
}

func identityEvent(tx *gorm.DB, identity *types.WeComIdentity, actor, action string) error {
	return tx.Create(&types.WeComIdentityEvent{ID: uuid.NewString(), IdentityID: identity.ID, ActorID: actor, Action: action, UserID: identity.UserID, Version: identity.Version, CreatedAt: time.Now()}).Error
}

func (r *WeComRepository) Bind(ctx context.Context, corp, previewHash, actor string) (*types.WeComIdentity, error) {
	var result types.WeComIdentity
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var flow types.WeComFlow
		if err := tx.Where("purpose = ? AND secret_hash = ? AND actor_id = ? AND expires_at > ? AND corp_id = ?", "bind", previewHash, actor, time.Now(), corp).First(&flow).Error; err != nil {
			return err
		}
		if err := lockAuthUser(tx, flow.UserID); err != nil {
			return err
		}
		if err := assertWeComAdmin(tx, actor); err != nil {
			return err
		}
		if err := consumeWeComFlow(tx, flow.ID, "bind", previewHash, ""); err != nil {
			return err
		}
		existing := tx.Where("corp_id = ? AND subject = ?", corp, flow.Subject).First(&result).Error
		switch {
		case errors.Is(existing, gorm.ErrRecordNotFound):
			result = types.WeComIdentity{ID: uuid.NewString(), CorpID: corp, Subject: flow.Subject, UserID: flow.UserID, DisplayName: flow.DisplayName, Status: "active", Version: 1}
			if err := tx.Create(&result).Error; err != nil {
				return ErrWeComConflict
			}
		case existing != nil:
			return existing
		default:
			if result.Status != "revoked" {
				return ErrWeComConflict
			}
			// Rebinding keeps the immutable identity ID and increments its version.
			// Old sessions remain invalid even if the old account is bound again.
			result.UserID, result.DisplayName, result.Status = flow.UserID, flow.DisplayName, "active"
			result.Version++
			update := tx.Model(&types.WeComIdentity{}).Where("id = ? AND status = ? AND version = ?", result.ID, "revoked", result.Version-1).Updates(map[string]any{"user_id": result.UserID, "display_name": result.DisplayName, "status": result.Status, "version": result.Version, "updated_at": time.Now()})
			if update.Error != nil || update.RowsAffected != 1 {
				return ErrWeComConflict
			}
		}
		return identityEvent(tx, &result, actor, "bind")
	})
	return &result, err
}

func (r *WeComRepository) SetStatus(ctx context.Context, corp, id, actor, status string, version int64) error {
	if status != "active" && status != "suspended" && status != "revoked" {
		return ErrWeComConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var identity types.WeComIdentity
		if err := tx.Where("id = ? AND corp_id = ?", id, corp).First(&identity).Error; err != nil {
			return err
		}
		if err := tx.Model(&types.User{}).Where("id = ?", identity.UserID).UpdateColumn("id", gorm.Expr("id")).Error; err != nil {
			return err
		}
		if err := assertWeComAdmin(tx, actor); err != nil {
			return err
		}
		update := tx.Model(&types.WeComIdentity{}).Where("id = ? AND version = ? AND status <> ?", id, version, "revoked").Updates(map[string]any{"status": status, "version": gorm.Expr("version + 1"), "updated_at": time.Now()})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrWeComConflict
		}
		if err := tx.Model(&types.AuthToken{}).Where("external_identity_id = ?", id).Update("is_revoked", true).Error; err != nil {
			return err
		}
		identity.Version, identity.Status = version+1, status
		return identityEvent(tx, &identity, actor, status)
	})
}

func (r *WeComRepository) Events(ctx context.Context, corp, id string) ([]types.WeComIdentityEvent, error) {
	var rows []types.WeComIdentityEvent
	err := r.db.WithContext(ctx).Model(&types.WeComIdentityEvent{}).Joins("JOIN we_com_identities i ON i.id = we_com_identity_events.identity_id").Where("i.corp_id = ? AND i.id = ?", corp, id).Order("we_com_identity_events.created_at DESC").Limit(100).Find(&rows).Error
	return rows, err
}

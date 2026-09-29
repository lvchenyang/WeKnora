package types

import "time"

// WeComIdentity links a corporate member to an existing local account. It never
// owns workspace roles; authorization continues to use UserID.
type WeComIdentity struct {
	ID          string    `json:"id" gorm:"primaryKey;type:varchar(36)"`
	CorpID      string    `json:"corp_id" gorm:"uniqueIndex:wecom_subject;uniqueIndex:wecom_user,where:status <> 'revoked'"`
	Subject     string    `json:"subject" gorm:"uniqueIndex:wecom_subject"`
	UserID      string    `json:"user_id" gorm:"uniqueIndex:wecom_user,where:status <> 'revoked'"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WeComFlow stores only digests of browser/state/exchange/preview secrets.
// Rows are consumed with a conditional DELETE inside the relevant transaction.
type WeComFlow struct {
	ID              string `gorm:"primaryKey;type:varchar(36)"`
	CorpID          string
	Purpose         string
	SecretHash      string `gorm:"uniqueIndex"`
	BrowserHash     string
	UserID          string
	IdentityID      string
	IdentityVersion int64
	Subject         string
	DisplayName     string
	ActorID         string
	ExpiresAt       time.Time `gorm:"index"`
}

type WeComIdentityEvent struct {
	ID         string    `json:"id" gorm:"primaryKey;type:varchar(36)"`
	IdentityID string    `json:"identity_id" gorm:"index"`
	ActorID    string    `json:"actor_id"`
	Action     string    `json:"action"`
	UserID     string    `json:"user_id"`
	Version    int64     `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
}

// WeComLoginGrant comes only from the server-side, browser-bound exchange.
type WeComLoginGrant struct {
	FlowID          string
	SecretHash      string
	BrowserHash     string
	UserID          string
	IdentityID      string
	IdentityVersion int64
}

// SessionIssue carries server-validated session provenance and a single-use
// refresh token or external login grant to consume atomically with the pair.
type SessionIssue struct {
	Source       *AuthToken
	RefreshToken string
	Grant        *WeComLoginGrant
}

type authSourceContextKey struct{}

// AuthSourceContextKey is set exclusively after bearer JWT authentication.
var AuthSourceContextKey = authSourceContextKey{}

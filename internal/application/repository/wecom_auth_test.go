package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func wecomDB(t *testing.T) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	var err error
	pgDSN := os.Getenv("WECOM_TEST_POSTGRES_DSN")
	if pgDSN != "" {
		admin, e := gorm.Open(postgres.New(postgres.Config{DSN: pgDSN, PreferSimpleProtocol: true}), &gorm.Config{})
		if e != nil {
			t.Fatal(e)
		}
		schema := "wecom_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if e := admin.Exec("CREATE SCHEMA " + schema).Error; e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE"); sqlDB, _ := admin.DB(); sqlDB.Close() })
		db, err = gorm.Open(postgres.New(postgres.Config{DSN: pgDSN + " search_path=" + schema, PreferSimpleProtocol: true}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	} else {
		db, err = gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")+"?_busy_timeout=5000&_foreign_keys=on"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	}
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	if pgDSN == "" {
		sqlDB.SetMaxOpenConns(1)
	} else {
		sqlDB.SetMaxOpenConns(8)
	}
	t.Cleanup(func() { sqlDB.Close() })
	// Exercise the production SQL migration, not an AutoMigrate approximation.
	for _, sql := range []string{
		`CREATE TABLE users (id VARCHAR(36) PRIMARY KEY, is_active BOOLEAN NOT NULL DEFAULT true, is_system_admin BOOLEAN NOT NULL DEFAULT false, deleted_at DATETIME)`,
		`CREATE TABLE auth_tokens (id VARCHAR(36) PRIMARY KEY,user_id VARCHAR(36),token TEXT,token_type TEXT,expires_at DATETIME,is_revoked BOOLEAN DEFAULT false,created_at DATETIME,updated_at DATETIME)`,
	} {
		if pgDSN != "" {
			sql = strings.ReplaceAll(sql, "DATETIME", "TIMESTAMPTZ")
		}
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	path := "../../../migrations/sqlite/000034_wecom_login.up.sql"
	if pgDSN != "" {
		path = "../../../migrations/versioned/000115_wecom_login.up.sql"
	}
	migration, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO users(id,is_active,is_system_admin) VALUES ('admin',true,true),('alice',true,false),('bob',true,false)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func identityFixture(t *testing.T, db *gorm.DB) *types.WeComIdentity {
	t.Helper()
	i := &types.WeComIdentity{ID: uuid.NewString(), CorpID: "corp", Subject: "alice", UserID: "alice", DisplayName: "Alice", Status: "active", Version: 1}
	if err := db.Create(i).Error; err != nil {
		t.Fatal(err)
	}
	return i
}
func grantFixture(t *testing.T, db *gorm.DB, i *types.WeComIdentity) *types.WeComLoginGrant {
	t.Helper()
	f := types.WeComFlow{ID: uuid.NewString(), Purpose: "ticket", SecretHash: uuid.NewString(), BrowserHash: "browser", UserID: i.UserID, IdentityID: i.ID, IdentityVersion: i.Version, ExpiresAt: time.Now().Add(time.Minute)}
	if err := db.Create(&f).Error; err != nil {
		t.Fatal(err)
	}
	return &types.WeComLoginGrant{FlowID: f.ID, SecretHash: f.SecretHash, BrowserHash: f.BrowserHash, UserID: i.UserID, IdentityID: i.ID, IdentityVersion: i.Version}
}
func sessionPair(i *types.WeComIdentity) (*types.AuthToken, *types.AuthToken) {
	family := uuid.NewString()
	a := &types.AuthToken{ID: uuid.NewString(), UserID: i.UserID, Token: uuid.NewString(), TokenType: "access_token", ExpiresAt: time.Now().Add(time.Hour), AuthMethod: "wecom", ExternalIdentityID: i.ID, ExternalIdentityVersion: i.Version, SessionFamilyID: family}
	b := *a
	b.ID = uuid.NewString()
	b.Token = uuid.NewString()
	b.TokenType = "refresh_token"
	return a, &b
}

func TestWeComAtomicExchange(t *testing.T) {
	db := wecomDB(t)
	repo := &authTokenRepository{db: db, wecomCorpID: "corp"}
	i := identityFixture(t, db)
	grant := grantFixture(t, db, i)
	a, b := sessionPair(i)
	b.ID = a.ID
	if repo.CreateTokenPair(context.Background(), a, b, types.SessionIssue{Grant: grant}) == nil {
		t.Fatal("expected second insert failure")
	}
	var count int64
	db.Model(&types.AuthToken{}).Count(&count)
	if count != 0 {
		t.Fatal("half session persisted")
	}
	db.Model(&types.WeComFlow{}).Count(&count)
	if count != 1 {
		t.Fatal("failed transaction consumed grant")
	}
	b.ID = uuid.NewString()
	if err := repo.CreateTokenPair(context.Background(), a, b, types.SessionIssue{Grant: grant}); err != nil {
		t.Fatal(err)
	}
	a, b = sessionPair(i)
	if repo.CreateTokenPair(context.Background(), a, b, types.SessionIssue{Grant: grant}) == nil {
		t.Fatal("grant replay accepted")
	}
}

func TestWeComRejectsInvalidGrants(t *testing.T) {
	for _, scenario := range []string{"browser", "expiry", "suspended", "version", "owner", "inactive", "corporation"} {
		t.Run(scenario, func(t *testing.T) {
			db := wecomDB(t)
			repo := &authTokenRepository{db: db, wecomCorpID: "corp"}
			i := identityFixture(t, db)
			grant := grantFixture(t, db, i)
			a, b := sessionPair(i)
			switch scenario {
			case "browser":
				grant.BrowserHash = "wrong"
			case "expiry":
				db.Model(&types.WeComFlow{}).Where("id = ?", grant.FlowID).Update("expires_at", time.Now().Add(-time.Second))
			case "suspended":
				db.Model(i).Update("status", "suspended")
			case "version":
				db.Model(i).Update("version", 2)
			case "owner":
				db.Model(i).Update("user_id", "bob")
			case "inactive":
				db.Table("users").Where("id = ?", "alice").Update("is_active", false)
			case "corporation":
				repo.wecomCorpID = "other"
			}
			if repo.CreateTokenPair(context.Background(), a, b, types.SessionIssue{Grant: grant}) == nil {
				t.Fatal("invalid grant accepted")
			}
			var count int64
			db.Model(&types.AuthToken{}).Count(&count)
			if count != 0 {
				t.Fatal("invalid session persisted")
			}
		})
	}
}

func TestWeComConcurrentExchangeAndRefresh(t *testing.T) {
	db := wecomDB(t)
	repo := &authTokenRepository{db: db, wecomCorpID: "corp"}
	i := identityFixture(t, db)
	grant := grantFixture(t, db, i)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, b := sessionPair(i)
			if repo.CreateTokenPair(context.Background(), a, b, types.SessionIssue{Grant: grant}) == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("exchange winners=%d", wins.Load())
	}
	var old types.AuthToken
	db.Where("token_type = ?", "refresh_token").First(&old)
	wins.Store(0)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, b := sessionPair(i)
			a.SessionFamilyID = old.SessionFamilyID
			b.SessionFamilyID = old.SessionFamilyID
			if repo.CreateTokenPair(context.Background(), a, b, types.SessionIssue{Source: &old, RefreshToken: old.Token}) == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("refresh winners=%d", wins.Load())
	}
}

func TestWeComStatusRevokesSessionsAndPendingGrants(t *testing.T) {
	db := wecomDB(t)
	repo := &authTokenRepository{db: db, wecomCorpID: "corp"}
	bindings := NewWeComRepository(db)
	i := identityFixture(t, db)
	grant := grantFixture(t, db, i)
	a, b := sessionPair(i)
	if err := repo.CreateTokenPair(context.Background(), a, b, types.SessionIssue{Grant: grant}); err != nil {
		t.Fatal(err)
	}
	pending := grantFixture(t, db, i)
	if err := bindings.SetStatus(context.Background(), "corp", i.ID, "admin", "suspended", 1); err != nil {
		t.Fatal(err)
	}
	if repo.ValidateTokenIdentity(context.Background(), a) == nil {
		t.Fatal("old access survived suspension")
	}
	if err := bindings.SetStatus(context.Background(), "corp", i.ID, "admin", "active", 2); err != nil {
		t.Fatal(err)
	}
	if repo.ValidateTokenIdentity(context.Background(), a) == nil {
		t.Fatal("restore resurrected old access")
	}
	x, y := sessionPair(i)
	if repo.CreateTokenPair(context.Background(), x, y, types.SessionIssue{Grant: pending}) == nil {
		t.Fatal("pending ticket survived version change")
	}
	if bindings.SetStatus(context.Background(), "corp", i.ID, "admin", "revoked", 1) == nil {
		t.Fatal("stale admin update accepted")
	}
	events, err := bindings.Events(context.Background(), "corp", i.ID)
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%v err=%v", events, err)
	}
}

func TestWeComBindingPreviewAndRebind(t *testing.T) {
	db := wecomDB(t)
	repo := NewWeComRepository(db)
	preview := func(user, subject string) string {
		id := uuid.NewString()
		f := types.WeComFlow{ID: id, Purpose: "bind", CorpID: "corp", SecretHash: id, ActorID: "admin", UserID: user, Subject: subject, ExpiresAt: time.Now().Add(time.Minute)}
		if err := db.Create(&f).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	token := preview("alice", "employee")
	if _, err := repo.Bind(context.Background(), "corp", token, "bob"); err == nil {
		t.Fatal("preview used by different actor")
	}
	i, err := repo.Bind(context.Background(), "corp", token, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Bind(context.Background(), "corp", token, "admin"); err == nil {
		t.Fatal("preview replay accepted")
	}
	if _, err := repo.Bind(context.Background(), "corp", preview("bob", "employee"), "admin"); err == nil {
		t.Fatal("active binding stolen")
	}
	if _, err := repo.Bind(context.Background(), "corp", preview("alice", "other"), "admin"); err == nil {
		t.Fatal("duplicate active local account binding accepted")
	}
	if err := repo.SetStatus(context.Background(), "corp", i.ID, "admin", "revoked", 1); err != nil {
		t.Fatal(err)
	}
	rebound, err := repo.Bind(context.Background(), "corp", preview("bob", "employee"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if rebound.ID != i.ID || rebound.Version != 3 || rebound.UserID != "bob" {
		t.Fatal("rebind identity/version incorrect")
	}
	if _, err := repo.Bind(context.Background(), "corp", preview("alice", "other"), "admin"); err != nil {
		t.Fatal("old local account cannot be corrected", err)
	}
}

func TestWeComSwitchCannotSubstituteAnotherSession(t *testing.T) {
	db := wecomDB(t)
	repo := &authTokenRepository{db: db, wecomCorpID: "corp"}
	i := identityFixture(t, db)
	first, refresh := sessionPair(i)
	if err := repo.CreateTokenPair(context.Background(), first, refresh, types.SessionIssue{Grant: grantFixture(t, db, i)}); err != nil {
		t.Fatal(err)
	}
	second, other := sessionPair(i)
	if err := repo.CreateTokenPair(context.Background(), second, other, types.SessionIssue{Grant: grantFixture(t, db, i)}); err != nil {
		t.Fatal(err)
	}
	a, b := sessionPair(i)
	a.SessionFamilyID = first.SessionFamilyID
	b.SessionFamilyID = first.SessionFamilyID
	if repo.CreateTokenPair(context.Background(), a, b, types.SessionIssue{Source: first, RefreshToken: other.Token}) == nil {
		t.Fatal("foreign family accepted")
	}
	if err := repo.CreateTokenPair(context.Background(), a, b, types.SessionIssue{Source: first, RefreshToken: refresh.Token}); err != nil {
		t.Fatal(err)
	}
}

func TestWeComMigrationRollbackPreservesAccounts(t *testing.T) {
	db := wecomDB(t)
	path := "../../../migrations/sqlite/000034_wecom_login.down.sql"
	if os.Getenv("WECOM_TEST_POSTGRES_DSN") != "" {
		path = "../../../migrations/versioned/000115_wecom_login.down.sql"
	}
	sql, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(sql)).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Table("users").Count(&count)
	if count != 3 {
		t.Fatal("rollback changed local accounts")
	}
	if db.Migrator().HasColumn("auth_tokens", "auth_method") {
		t.Fatal("rollback left token provenance column")
	}
	if db.Migrator().HasTable("we_com_identities") {
		t.Fatal("rollback left identities table")
	}
}

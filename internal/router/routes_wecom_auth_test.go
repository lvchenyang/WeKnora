package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func TestWeComAdminRoutesDenyNonAdminsAndAPIKeys(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		admin, apiKey, platform bool
		want                    int
	}{
		{"ordinary JWT", false, false, false, 403}, {"workspace key", true, true, false, 403}, {"platform key", true, true, true, 403}, {"system admin JWT", true, false, false, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			disabled := false
			cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &disabled}}
			h := handler.NewWeComAuthHandler(service.NewWeComService(cfg, nil, nil), nil)
			g := &rbacGuards{cfg: cfg}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				ctx := context.WithValue(c.Request.Context(), types.SystemAdminContextKey, tc.admin)
				if tc.apiKey {
					scope := types.TenantAPIKeyScope{FullAccess: true}
					if tc.platform {
						scope.ScopeType = types.APIKeyScopePlatform
					}
					ctx = types.WithTenantAPIKeyScope(ctx, scope)
				}
				c.Request = c.Request.WithContext(ctx)
			}, g.ensureAPIKeyAuthorizer().Middleware())
			RegisterWeComAuthRoutes(r.Group("/api/v1"), h, g)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/system/admin/wecom/bindings", nil))
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

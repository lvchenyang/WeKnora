package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterWeComAuthRoutes(r *gin.RouterGroup, h *handler.WeComAuthHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	r.GET("/auth/wecom/config", h.Config)
	public := r.Group("/auth/wecom", middleware.WeComAuthRateLimit())
	public.GET("/start", h.Start)
	public.GET("/callback", h.Callback)
	public.POST("/exchange", h.Exchange)
	// Unregistered in the API-key capability registry: JWT system admins only.
	admin := r.Group("/system/admin/wecom", g.SystemAdmin())
	admin.GET("/bindings", h.List)
	admin.POST("/preview", h.Preview)
	admin.POST("/bindings", h.Bind)
	admin.PUT("/bindings/:id", h.SetStatus)
	admin.GET("/bindings/:id/events", h.Events)
}

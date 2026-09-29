package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/handler/dto"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const wecomBrowserCookie = "__Host-weknora-wecom-browser"

func wecomTicketCookie(id string) string { return "__Host-weknora-wecom-ticket-" + id }

type WeComAuthHandler struct {
	service *service.WeComService
	users   interfaces.UserService
}

func NewWeComAuthHandler(s *service.WeComService, users interfaces.UserService) *WeComAuthHandler {
	return &WeComAuthHandler{service: s, users: users}
}
func (h *WeComAuthHandler) enabled() bool { return Edition != "lite" && h.service.Config.Enable }

func wecomHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
}
func wecomCookie(c *gin.Context, name, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}
func (h *WeComAuthHandler) requireEnabled(c *gin.Context) bool {
	wecomHeaders(c)
	if !h.enabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "wecom_disabled"})
		return false
	}
	return true
}
func (h *WeComAuthHandler) Config(c *gin.Context) {
	wecomHeaders(c)
	c.JSON(http.StatusOK, gin.H{"enabled": h.enabled()})
}
func (h *WeComAuthHandler) Start(c *gin.Context) {
	if !h.requireEnabled(c) {
		return
	}
	browser, _ := c.Cookie(wecomBrowserCookie)
	if len(browser) != 64 {
		var err error
		browser, err = service.WeComSecret()
		if err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
	}
	target, err := h.service.Start(c.Request.Context(), browser, strings.Contains(strings.ToLower(c.GetHeader("User-Agent")), "wxwork"))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "wecom_unavailable"})
		return
	}
	wecomCookie(c, wecomBrowserCookie, browser, 600)
	c.Redirect(http.StatusFound, target)
}
func (h *WeComAuthHandler) Callback(c *gin.Context) {
	if !h.requireEnabled(c) {
		return
	}
	browser, _ := c.Cookie(wecomBrowserCookie)
	id, ticket, err := h.service.Callback(c.Request.Context(), c.Query("code"), c.Query("state"), browser)
	target := h.service.Config.PublicOrigin + "/login/wecom/complete"
	if err != nil {
		c.Redirect(http.StatusSeeOther, target+"?error=wecom_failed")
		return
	}
	wecomCookie(c, wecomTicketCookie(id), ticket, 60)
	c.Redirect(http.StatusSeeOther, target+"?flow_id="+id)
}
func (h *WeComAuthHandler) Exchange(c *gin.Context) {
	if !h.requireEnabled(c) {
		return
	}
	if c.GetHeader("Origin") != h.service.Config.PublicOrigin {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid_origin"})
		return
	}
	var req struct {
		FlowID string `json:"flow_id" binding:"required,uuid"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	ticket, _ := c.Cookie(wecomTicketCookie(req.FlowID))
	browser, _ := c.Cookie(wecomBrowserCookie)
	response, err := h.service.Exchange(c.Request.Context(), req.FlowID, ticket, browser)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "wecom_failed"})
		return
	}
	wecomCookie(c, wecomTicketCookie(req.FlowID), "", -1)
	c.JSON(http.StatusOK, dto.NewAuthLoginResponse(response))
}

func (h *WeComAuthHandler) Preview(c *gin.Context) {
	if !h.requireEnabled(c) {
		return
	}
	var req struct {
		Email   string `json:"email" binding:"required,email,max=255"`
		Subject string `json:"subject" binding:"required,max=64"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	actor, err := h.users.GetCurrentUser(c.Request.Context())
	if err != nil || actor == nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	preview, err := h.service.Preview(c.Request.Context(), actor.ID, req.Email, req.Subject)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "wecom_preview_failed"})
		return
	}
	c.JSON(http.StatusOK, preview)
}
func (h *WeComAuthHandler) Bind(c *gin.Context) {
	if !h.requireEnabled(c) {
		return
	}
	var req struct {
		PreviewToken string `json:"preview_token" binding:"required,len=64"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	actor, err := h.users.GetCurrentUser(c.Request.Context())
	if err != nil || actor == nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	row, err := h.service.Repository.Bind(c.Request.Context(), h.service.Config.CorpID, service.WeComDigest(req.PreviewToken), actor.ID)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "wecom_binding_conflict"})
		return
	}
	c.JSON(http.StatusOK, row)
}
func (h *WeComAuthHandler) List(c *gin.Context) {
	if !h.requireEnabled(c) {
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	rows, err := h.service.Repository.List(c.Request.Context(), h.service.Config.CorpID, offset)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	users, err := h.users.GetUsersByIDs(c.Request.Context(), ids)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusOK, gin.H{"bindings": rows, "users": users, "corp_id": h.service.Config.CorpID})
}
func (h *WeComAuthHandler) SetStatus(c *gin.Context) {
	if !h.requireEnabled(c) {
		return
	}
	var req struct {
		Status  string `json:"status" binding:"required,oneof=active suspended revoked"`
		Version int64  `json:"version" binding:"required,min=1"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	actor, err := h.users.GetCurrentUser(c.Request.Context())
	if err != nil || actor == nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	if err := h.service.Repository.SetStatus(c.Request.Context(), h.service.Config.CorpID, c.Param("id"), actor.ID, req.Status, req.Version); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "wecom_binding_conflict"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
func (h *WeComAuthHandler) Events(c *gin.Context) {
	if !h.requireEnabled(c) {
		return
	}
	if _, err := uuid.Parse(c.Param("id")); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	rows, err := h.service.Repository.Events(c.Request.Context(), h.service.Config.CorpID, c.Param("id"))
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": rows})
}

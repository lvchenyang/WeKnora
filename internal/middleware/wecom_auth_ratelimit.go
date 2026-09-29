package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// A corporate NAT may serve many employees. Keep the three-step WeCom flow
// separate from invitation/registration buckets, with 120 requests/minute/IP.
var wecomAuthLimiter = newIPRateLimiter(time.Minute, 120)

func WeComAuthRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !wecomAuthLimiter.allow(c.ClientIP()) {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too_many_requests"})
			return
		}
		c.Next()
	}
}

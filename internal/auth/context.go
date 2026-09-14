package auth

import (
	"github.com/gin-gonic/gin"
)

const (
	ContextUserIDKey   = "userID"
	ContextUsernameKey = "username"
)

func SetUser(c *gin.Context, userID uint, username string) {
	c.Set(ContextUserIDKey, userID)
	c.Set(ContextUsernameKey, username)
}

func UserID(c *gin.Context) (uint, bool) {
	v, ok := c.Get(ContextUserIDKey)
	if !ok {
		return 0, false
	}
	id, ok := v.(uint)
	return id, ok
}

func Username(c *gin.Context) (string, bool) {
	v, ok := c.Get(ContextUsernameKey)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

package modules

import (
	"github.com/gin-gonic/gin"
	"github.com/hello-coder/hello-coder/internal/auth"
	"github.com/hello-coder/hello-coder/internal/secret"
	"gorm.io/gorm"
)

// Deps is shared infrastructure injected into every tool module.
type Deps struct {
	DB      *gorm.DB
	Tokens  *auth.TokenManager
	Secrets *secret.Box
	Auth    gin.HandlerFunc // RequireAuth or BypassAuth
}

// Module is a pluggable tool (Kafka, ES, ...).
type Module interface {
	Name() string
	Register(api *gin.RouterGroup, deps Deps)
}

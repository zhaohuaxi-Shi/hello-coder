package redis

import (
	"github.com/gin-gonic/gin"
	"github.com/hello-coder/hello-coder/internal/modules"
)

// Module is the Redis (RDS) client tool.
type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "redis" }

func (m *Module) Register(api *gin.RouterGroup, deps modules.Deps) {
	h := &handler{db: deps.DB, secrets: deps.Secrets}
	g := api.Group("/redis", deps.Auth)
	{
		g.GET("/conns", h.listConns)
		g.POST("/conns", h.createConn)
		g.POST("/conns/test", h.testConn)
		g.GET("/status", h.connStatus)
		g.GET("/conns/:id", h.getConn)
		g.PUT("/conns/:id", h.updateConn)
		g.DELETE("/conns/:id", h.deleteConn)
		g.POST("/conns/:id/ping", h.ping)
		g.GET("/conns/:id/info", h.connInfo)

		g.GET("/conns/:id/dbs", h.listDBs)
		g.GET("/conns/:id/keys/scan", h.scanKeys)
		g.POST("/conns/:id/keys/get", h.getKey)
		g.POST("/conns/:id/keys", h.setKey)
		g.POST("/conns/:id/keys/expire", h.expireKey)
		g.POST("/conns/:id/keys/delete", h.deleteKeys)
	}
}

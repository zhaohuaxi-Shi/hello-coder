package kafka

import (
	"github.com/gin-gonic/gin"
	"github.com/hello-coder/hello-coder/internal/modules"
)

// Module is the Kafka client tool.
type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "kafka" }

func (m *Module) Register(api *gin.RouterGroup, deps modules.Deps) {
	h := &handler{db: deps.DB, secrets: deps.Secrets}
	g := api.Group("/kafka", deps.Auth)
	{
		g.GET("/conns", h.listConns)
		g.POST("/conns", h.createConn)
		g.POST("/conns/test", h.testConn)
		g.GET("/status", h.connStatus)
		g.GET("/conns/:id", h.getConn)
		g.PUT("/conns/:id", h.updateConn)
		g.DELETE("/conns/:id", h.deleteConn)
		g.POST("/conns/:id/ping", h.ping)
		g.GET("/conns/:id/cluster", h.clusterDetail)

		g.GET("/conns/:id/topics", h.listTopics)
		g.GET("/conns/:id/topics/*topic", h.topicDetail)
		g.DELETE("/conns/:id/topics/*topic", h.deleteTopic)
		g.POST("/conns/:id/consume", h.consume)
		g.POST("/conns/:id/produce", h.produce)
	}
}

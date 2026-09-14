package es

import (
	"github.com/gin-gonic/gin"
	"github.com/hello-coder/hello-coder/internal/modules"
)

// Module is the Elasticsearch client tool.
type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "es" }

func (m *Module) Register(api *gin.RouterGroup, deps modules.Deps) {
	h := &handler{db: deps.DB, secrets: deps.Secrets}
	g := api.Group("/es", deps.Auth)
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

		g.GET("/conns/:id/indices", h.listIndices)
		g.POST("/conns/:id/indices", h.createIndex)
		g.GET("/conns/:id/indices/:index", h.indexStructure)
		g.DELETE("/conns/:id/indices/:index", h.deleteIndex)
		g.POST("/conns/:id/indices/:index/clear", h.clearIndex)
		g.POST("/conns/:id/indices/:index/aliases", h.setAlias)
		g.POST("/conns/:id/indices/:index/clone", h.cloneIndex)
		g.POST("/conns/:id/indices/:index/rebuild", h.rebuildIndex)
		g.GET("/conns/:id/indices/:index/fields", h.listIndexFields)
		g.POST("/conns/:id/indices/:index/docs/search", h.searchDocs)
		g.POST("/conns/:id/indices/:index/docs", h.createDoc)
		g.PUT("/conns/:id/indices/:index/docs/:docId", h.updateDoc)
		g.POST("/conns/:id/indices/:index/docs/bulk-delete", h.bulkDeleteDocs)
		g.POST("/conns/:id/indices/:index/docs/bulk-update", h.bulkUpdateDocs)
	}
}

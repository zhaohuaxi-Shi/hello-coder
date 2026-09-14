package connection

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hello-coder/hello-coder/internal/modules"
	"github.com/hello-coder/hello-coder/internal/response"
	"gorm.io/gorm"
)

// Module manages datasource connection configs shared by Kafka / ES / etc.
type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "connection" }

func (m *Module) Register(api *gin.RouterGroup, deps modules.Deps) {
	h := &handler{db: deps.DB}
	g := api.Group("/connections")
	{
		g.GET("", h.list)
		g.GET("/:id", h.get)
		g.POST("", h.create)
		g.PUT("/:id", h.update)
		g.DELETE("/:id", h.delete)
	}
}

type handler struct {
	db *gorm.DB
}

// Stubs keep the route tree visible; real CRUD comes next.
func (h *handler) list(c *gin.Context) {
	_ = h.db
	response.OK(c, []any{})
}

func (h *handler) get(c *gin.Context) {
	response.NotFound(c, "not implemented")
}

func (h *handler) create(c *gin.Context) {
	response.Fail(c, http.StatusNotImplemented, 501, "not implemented")
}

func (h *handler) update(c *gin.Context) {
	response.Fail(c, http.StatusNotImplemented, 501, "not implemented")
}

func (h *handler) delete(c *gin.Context) {
	response.Fail(c, http.StatusNotImplemented, 501, "not implemented")
}

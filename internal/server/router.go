package server

import (
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hello-coder/hello-coder/internal/auth"
	"github.com/hello-coder/hello-coder/internal/middleware"
	"github.com/hello-coder/hello-coder/internal/modules"
	authmod "github.com/hello-coder/hello-coder/internal/modules/auth"
	"github.com/hello-coder/hello-coder/internal/modules/es"
	"github.com/hello-coder/hello-coder/internal/modules/kafka"
	redismod "github.com/hello-coder/hello-coder/internal/modules/redis"
	"github.com/hello-coder/hello-coder/internal/response"
	"github.com/hello-coder/hello-coder/internal/secret"
	"github.com/hello-coder/hello-coder/internal/version"
	"github.com/hello-coder/hello-coder/web"
	"gorm.io/gorm"
)

type RouterOpts struct {
	AuthRequired bool
	LocalUserID  uint
	LocalUser    string
}

func NewRouter(db *gorm.DB, tokens *auth.TokenManager, secrets *secret.Box, staticDir string, opts RouterOpts) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestLog(), middleware.CORS())

	r.GET("/api/health", func(c *gin.Context) {
		info := version.Info()
		info["status"] = "up"
		response.OK(c, info)
	})

	api := r.Group("/api/v1")

	authMW := middleware.RequireAuth(tokens)
	if !opts.AuthRequired {
		authMW = middleware.BypassAuth(opts.LocalUserID, opts.LocalUser)
	}

	deps := modules.Deps{DB: db, Tokens: tokens, Secrets: secrets, Auth: authMW}

	api.GET("/meta", func(c *gin.Context) {
		info := version.Info()
		info["authRequired"] = opts.AuthRequired
		response.OK(c, info)
	})

	// Public key for browser RSA-OAEP (no auth; key is public by design).
	api.GET("/crypto/public-key", func(c *gin.Context) {
		response.OK(c, secrets.PublicKeyInfo())
	})

	regs := []modules.Module{
		authmod.New(tokens),
		kafka.New(),
		es.New(),
		redismod.New(),
	}
	for _, m := range regs {
		m.Register(api, deps)
	}

	api.GET("/me", authMW, func(c *gin.Context) {
		uid, _ := auth.UserID(c)
		name, _ := auth.Username(c)
		response.OK(c, gin.H{"id": uid, "username": name})
	})

	mountStatic(r, staticDir)
	return r
}

func mountStatic(r *gin.Engine, staticDir string) {
	if dir := strings.TrimSpace(staticDir); dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			panic(err)
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			panic("HELLO_CODER_STATIC_DIR is not a directory: " + abs)
		}
		slog.Info("serving static from disk (dev)", "dir", abs)
		r.Use(func(c *gin.Context) {
			if strings.HasPrefix(c.Request.URL.Path, "/assets/") ||
				c.Request.URL.Path == "/" ||
				c.Request.URL.Path == "/app" ||
				c.Request.URL.Path == "/app.html" {
				c.Header("Cache-Control", "no-store")
			}
			c.Next()
		})
		serveDiskHTML := func(name string) gin.HandlerFunc {
			return func(c *gin.Context) {
				c.File(filepath.Join(abs, name))
			}
		}
		r.GET("/", serveDiskHTML("index.html"))
		r.GET("/app.html", serveDiskHTML("app.html"))
		r.GET("/app", serveDiskHTML("app.html"))
		r.Static("/assets", filepath.Join(abs, "assets"))
		return
	}

	staticRoot, err := fs.Sub(web.Static, "static")
	if err != nil {
		panic(err)
	}
	assetsFS, err := fs.Sub(staticRoot, "assets")
	if err != nil {
		panic(err)
	}

	serveHTML := func(name string) gin.HandlerFunc {
		return func(c *gin.Context) {
			data, err := fs.ReadFile(staticRoot, name)
			if err != nil {
				c.String(http.StatusNotFound, "page missing")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
		}
	}

	r.GET("/", serveHTML("index.html"))
	r.GET("/app.html", serveHTML("app.html"))
	r.GET("/app", serveHTML("app.html"))
	r.StaticFS("/assets", http.FS(assetsFS))
}

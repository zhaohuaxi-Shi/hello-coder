package authmod

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/hello-coder/hello-coder/internal/auth"
	"github.com/hello-coder/hello-coder/internal/model"
	"github.com/hello-coder/hello-coder/internal/modules"
	"github.com/hello-coder/hello-coder/internal/response"
	"gorm.io/gorm"
)

type Module struct {
	tokens *auth.TokenManager
}

func New(tokens *auth.TokenManager) *Module {
	return &Module{tokens: tokens}
}

func (m *Module) Name() string { return "auth" }

func (m *Module) Register(api *gin.RouterGroup, deps modules.Deps) {
	h := &handler{db: deps.DB, tokens: m.tokens}
	g := api.Group("/auth")
	{
		g.POST("/register", h.register)
		g.POST("/login", h.login)
	}
}

type handler struct {
	db     *gorm.DB
	tokens *auth.TokenManager
}

type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *handler) register(c *gin.Context) {
	var req authRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	username := strings.TrimSpace(req.Username)
	password := req.Password
	if err := validateCredentials(username, password); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	var exists int64
	if err := h.db.Model(&model.User{}).Where("username = ?", username).Count(&exists).Error; err != nil {
		response.Internal(c, "failed to query user")
		return
	}
	if exists > 0 {
		response.Fail(c, http.StatusConflict, 409, "username already exists")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		response.Internal(c, "failed to hash password")
		return
	}
	user := model.User{Username: username, Password: hash}
	if err := h.db.Create(&user).Error; err != nil {
		response.Internal(c, "registration failed")
		return
	}

	token, err := h.tokens.Issue(user.ID, user.Username)
	if err != nil {
		response.Internal(c, "failed to issue token")
		return
	}
	response.OK(c, gin.H{
		"token": token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

func (h *handler) login(c *gin.Context) {
	var req authRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		response.BadRequest(c, "username and password are required")
		return
	}

	var user model.User
	err := h.db.Where("username = ?", username).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Fail(c, http.StatusUnauthorized, 401, "invalid username or password")
		return
	}
	if err != nil {
		response.Internal(c, "failed to query user")
		return
	}
	if !auth.CheckPassword(user.Password, req.Password) {
		response.Fail(c, http.StatusUnauthorized, 401, "invalid username or password")
		return
	}

	token, err := h.tokens.Issue(user.ID, user.Username)
	if err != nil {
		response.Internal(c, "failed to issue token")
		return
	}
	response.OK(c, gin.H{
		"token": token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

func validateCredentials(username, password string) error {
	n := utf8.RuneCountInString(username)
	if n < 3 || n > 32 {
		return errors.New("username must be 3–32 characters")
	}
	for _, r := range username {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return errors.New("username allows letters, digits and underscore only")
	}
	if utf8.RuneCountInString(password) < 6 {
		return errors.New("password must be at least 6 characters")
	}
	return nil
}

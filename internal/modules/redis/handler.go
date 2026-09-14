package redis

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hello-coder/hello-coder/internal/auth"
	"github.com/hello-coder/hello-coder/internal/model"
	"github.com/hello-coder/hello-coder/internal/response"
	"github.com/hello-coder/hello-coder/internal/secret"
	"gorm.io/gorm"
)

type handler struct {
	db      *gorm.DB
	secrets *secret.Box
}

type connDTO struct {
	ID            uint   `json:"id"`
	UserID        uint   `json:"userId"`
	Name          string `json:"name"`
	Mode          string `json:"mode"`
	Addresses     string `json:"addresses"`
	MasterName    string `json:"masterName"`
	DB            int    `json:"db"`
	Username      string `json:"username"`
	Version       string `json:"version"`
	Description   string `json:"description"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
	Available     *bool  `json:"available,omitempty"`
	StatusMessage string `json:"statusMessage,omitempty"`
}

type connRequest struct {
	ID          uint   `json:"id"` // optional; used by test to reuse stored password
	Name        string `json:"name"`
	Mode        string `json:"mode"`
	Addresses   string `json:"addresses"`
	MasterName  string `json:"masterName"`
	DB          int    `json:"db"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Description string `json:"description"`
}

func toDTO(c model.RedisConn) connDTO {
	return connDTO{
		ID:          c.ID,
		UserID:      c.UserID,
		Name:        c.Name,
		Mode:        c.Mode,
		Addresses:   c.Addresses,
		MasterName:  c.MasterName,
		DB:          c.DB,
		Username:    c.Username,
		Version:     c.Version,
		Description: c.Description,
		CreatedAt:   c.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:   c.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

func normalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModeCluster:
		return ModeCluster
	case ModeSentinel:
		return ModeSentinel
	default:
		return ModeStandalone
	}
}

func (h *handler) listConns(c *gin.Context) {
	uid, _ := auth.UserID(c)
	var rows []model.RedisConn
	if err := h.db.Where("user_id = ?", uid).Order("id desc").Find(&rows).Error; err != nil {
		response.Internal(c, "failed to list connections")
		return
	}
	check := false
	if v := c.Query("check"); v == "1" || v == "true" {
		check = true
	}
	out := make([]connDTO, 0, len(rows))
	for _, row := range rows {
		dto := toDTO(row)
		if check {
			use, err := h.openConn(&row)
			if err != nil {
				falseVal := false
				dto.Available = &falseVal
				dto.StatusMessage = err.Error()
			} else {
				ping := Ping(use)
				dto.Available = &ping.Available
				dto.StatusMessage = ping.Message
			}
		}
		out = append(out, dto)
	}
	response.OK(c, out)
}

func (h *handler) getConn(c *gin.Context) {
	row, ok := h.loadOwned(c)
	if !ok {
		return
	}
	response.OK(c, toDTO(*row))
}

func (h *handler) createConn(c *gin.Context) {
	uid, _ := auth.UserID(c)
	var req connRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	if err := validateConnReq(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	plainPass, err := h.secrets.UnwrapTransport(req.Password)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	sealed, err := h.secrets.Seal(plainPass)
	if err != nil {
		response.Internal(c, "failed to seal password")
		return
	}
	mode := normalizeMode(req.Mode)
	dbIdx := req.DB
	if mode == ModeCluster {
		dbIdx = 0
	}
	row := model.RedisConn{
		UserID:      uid,
		Name:        strings.TrimSpace(req.Name),
		Mode:        mode,
		Addresses:   strings.TrimSpace(req.Addresses),
		MasterName:  strings.TrimSpace(req.MasterName),
		DB:          dbIdx,
		Username:    strings.TrimSpace(req.Username),
		Password:    sealed,
		Description: strings.TrimSpace(req.Description),
	}
	// Capture version once on save (best-effort; empty if unreachable).
	row.Version = h.fetchVersionPlain(&row, plainPass)
	if err := h.db.Create(&row).Error; err != nil {
		response.Internal(c, "failed to create connection")
		return
	}
	response.OK(c, toDTO(row))
}

func (h *handler) updateConn(c *gin.Context) {
	row, ok := h.loadOwned(c)
	if !ok {
		return
	}
	var req connRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	if err := validateConnReq(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	mode := normalizeMode(req.Mode)
	dbIdx := req.DB
	if mode == ModeCluster {
		dbIdx = 0
	}
	row.Name = strings.TrimSpace(req.Name)
	row.Mode = mode
	row.Addresses = strings.TrimSpace(req.Addresses)
	row.MasterName = strings.TrimSpace(req.MasterName)
	row.DB = dbIdx
	row.Username = strings.TrimSpace(req.Username)
	row.Description = strings.TrimSpace(req.Description)

	plainPass := ""
	if req.Password != "" {
		var err error
		plainPass, err = h.secrets.UnwrapTransport(req.Password)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		sealed, err := h.secrets.Seal(plainPass)
		if err != nil {
			response.Internal(c, "failed to seal password")
			return
		}
		row.Password = sealed
	} else {
		opened, err := h.secrets.Open(row.Password)
		if err != nil {
			response.Internal(c, "failed to decrypt connection secret")
			return
		}
		plainPass = opened
	}

	row.Version = h.fetchVersionPlain(row, plainPass)
	if err := h.db.Save(row).Error; err != nil {
		response.Internal(c, "failed to update connection")
		return
	}
	InvalidateConnClients(row.ID)
	response.OK(c, toDTO(*row))
}

func (h *handler) deleteConn(c *gin.Context) {
	row, ok := h.loadOwned(c)
	if !ok {
		return
	}
	id := row.ID
	if err := h.db.Delete(row).Error; err != nil {
		response.Internal(c, "failed to delete connection")
		return
	}
	InvalidateConnClients(id)
	response.OK(c, gin.H{"id": id})
}

func (h *handler) ping(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	response.OK(c, Ping(row))
}

func (h *handler) connStatus(c *gin.Context) {
	uid, _ := auth.UserID(c)
	var rows []model.RedisConn
	if err := h.db.Where("user_id = ?", uid).Find(&rows).Error; err != nil {
		response.Internal(c, "failed to list connections")
		return
	}
	response.OK(c, summarizeConns(rows, h.openConn))
}

func (h *handler) connInfo(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	detail := FetchConnDetail(row)
	if !detail.Available {
		response.Fail(c, 502, 502, "failed to fetch redis info: "+detail.Message)
		return
	}
	response.OK(c, detail)
}

// testConn probes connectivity from form fields without saving.
func (h *handler) testConn(c *gin.Context) {
	uid, _ := auth.UserID(c)
	var req connRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Addresses) == "" {
		response.BadRequest(c, "addresses is required")
		return
	}
	// Reuse create/update rules for mode-specific required fields.
	if err := validateConnReq(&connRequest{
		Name:       "test",
		Mode:       req.Mode,
		Addresses:  req.Addresses,
		MasterName: req.MasterName,
		DB:         req.DB,
	}); err != nil {
		// name is dummy for test; strip name-related if any
		response.BadRequest(c, err.Error())
		return
	}
	mode := normalizeMode(req.Mode)
	plainPass, err := h.secrets.UnwrapTransport(req.Password)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	dbIdx := req.DB
	if mode == ModeCluster {
		dbIdx = 0
	}
	row := model.RedisConn{
		Mode:       mode,
		Addresses:  strings.TrimSpace(req.Addresses),
		MasterName: strings.TrimSpace(req.MasterName),
		DB:         dbIdx,
		Username:   strings.TrimSpace(req.Username),
		Password:   plainPass,
	}
	if req.ID > 0 && plainPass == "" {
		var stored model.RedisConn
		if err := h.db.Where("id = ? AND user_id = ?", req.ID, uid).First(&stored).Error; err == nil {
			opened, err := h.openConn(&stored)
			if err != nil {
				response.Internal(c, "failed to decrypt stored password")
				return
			}
			row.Password = opened.Password
			if row.Username == "" {
				row.Username = stored.Username
			}
		}
	}
	response.OK(c, Ping(&row))
}

// fetchVersionPlain builds a temporary conn with plaintext password and reads redis_version.
func (h *handler) fetchVersionPlain(row *model.RedisConn, plainPass string) string {
	probe := *row
	probe.Password = plainPass
	return FetchVersion(&probe)
}

func (h *handler) loadOwned(c *gin.Context) (*model.RedisConn, bool) {
	uid, _ := auth.UserID(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.BadRequest(c, "invalid connection id")
		return nil, false
	}
	var row model.RedisConn
	err = h.db.Where("id = ? AND user_id = ?", id, uid).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.NotFound(c, "connection not found")
		return nil, false
	}
	if err != nil {
		response.Internal(c, "failed to query connection")
		return nil, false
	}
	return &row, true
}

func (h *handler) openConn(row *model.RedisConn) (*model.RedisConn, error) {
	cp := *row
	plain, err := h.secrets.Open(cp.Password)
	if err != nil {
		return nil, err
	}
	cp.Password = plain
	return &cp, nil
}

func (h *handler) loadOwnedForUse(c *gin.Context) (*model.RedisConn, bool) {
	row, ok := h.loadOwned(c)
	if !ok {
		return nil, false
	}
	use, err := h.openConn(row)
	if err != nil {
		response.Internal(c, "failed to decrypt connection secret")
		return nil, false
	}
	return use, true
}

func validateConnReq(req *connRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return errors.New("name is required")
	}
	mode := normalizeMode(req.Mode)
	req.Mode = mode

	addrs := splitAddrs(req.Addresses)
	if len(addrs) == 0 {
		switch mode {
		case ModeCluster:
			return errors.New("cluster mode requires at least one seed node (host:port)")
		case ModeSentinel:
			return errors.New("sentinel mode requires at least one sentinel address (host:port)")
		default:
			return errors.New("standalone mode requires address (host:port)")
		}
	}
	for _, a := range addrs {
		if !looksLikeHostPort(a) {
			return errors.New("invalid address format: " + a + " (expected host:port)")
		}
	}

	switch mode {
	case ModeStandalone:
		if len(addrs) != 1 {
			return errors.New("standalone mode accepts exactly one address")
		}
		req.MasterName = ""
		if req.DB < 0 {
			return errors.New("db must be >= 0")
		}
	case ModeCluster:
		req.MasterName = ""
		req.DB = 0
	case ModeSentinel:
		if strings.TrimSpace(req.MasterName) == "" {
			return errors.New("sentinel mode requires master name")
		}
		if req.DB < 0 {
			return errors.New("db must be >= 0")
		}
	}
	req.Addresses = strings.Join(addrs, ",")
	return nil
}

func looksLikeHostPort(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	if strings.HasPrefix(addr, "[") {
		// [ipv6]:port
		end := strings.Index(addr, "]:")
		if end <= 1 {
			return false
		}
		port := addr[end+2:]
		n, err := strconv.Atoi(port)
		return err == nil && n > 0 && n <= 65535
	}
	i := strings.LastIndex(addr, ":")
	if i <= 0 || i == len(addr)-1 {
		return false
	}
	host := strings.TrimSpace(addr[:i])
	port := addr[i+1:]
	if host == "" {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
}

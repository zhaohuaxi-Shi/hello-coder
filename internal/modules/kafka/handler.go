package kafka

import (
	"errors"
	"net/http"
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
	ID               uint   `json:"id"`
	UserID           uint   `json:"userId"`
	Name             string `json:"name"`
	Brokers          string `json:"brokers"`
	SecurityProtocol string `json:"securityProtocol"`
	SASLMechanism    string `json:"saslMechanism"`
	SASLUsername     string `json:"saslUsername"`
	Description      string `json:"description"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
	Available        *bool  `json:"available,omitempty"`
	StatusMessage    string `json:"statusMessage,omitempty"`
}

type connRequest struct {
	ID               uint   `json:"id"` // optional; used by test to reuse stored password
	Name             string `json:"name"`
	Brokers          string `json:"brokers"`
	SecurityProtocol string `json:"securityProtocol"`
	SASLMechanism    string `json:"saslMechanism"`
	SASLUsername     string `json:"saslUsername"`
	SASLPassword     string `json:"saslPassword"`
	Description      string `json:"description"`
}

func toDTO(c model.KafkaConn) connDTO {
	return connDTO{
		ID:               c.ID,
		UserID:           c.UserID,
		Name:             c.Name,
		Brokers:          c.Brokers,
		SecurityProtocol: c.SecurityProtocol,
		SASLMechanism:    c.SASLMechanism,
		SASLUsername:     c.SASLUsername,
		Description:      c.Description,
		CreatedAt:        c.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:        c.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

func (h *handler) listConns(c *gin.Context) {
	uid, _ := auth.UserID(c)
	var rows []model.KafkaConn
	if err := h.db.Where("user_id = ?", uid).Order("id desc").Find(&rows).Error; err != nil {
		response.Internal(c, "failed to list connections")
		return
	}
	check := c.Query("check") == "1" || c.Query("check") == "true"
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
	if err := validateConnReq(&req, true); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	plainPass, err := h.secrets.UnwrapTransport(req.SASLPassword)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	sealed, err := h.secrets.Seal(plainPass)
	if err != nil {
		response.Internal(c, "failed to seal password")
		return
	}
	row := model.KafkaConn{
		UserID:           uid,
		Name:             strings.TrimSpace(req.Name),
		Brokers:          strings.TrimSpace(req.Brokers),
		SecurityProtocol: defaultProtocol(req.SecurityProtocol),
		SASLMechanism:    strings.TrimSpace(req.SASLMechanism),
		SASLUsername:     strings.TrimSpace(req.SASLUsername),
		SASLPassword:     sealed,
		Description:      strings.TrimSpace(req.Description),
	}
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
	if err := validateConnReq(&req, false); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	row.Name = strings.TrimSpace(req.Name)
	row.Brokers = strings.TrimSpace(req.Brokers)
	row.SecurityProtocol = defaultProtocol(req.SecurityProtocol)
	row.SASLMechanism = strings.TrimSpace(req.SASLMechanism)
	row.SASLUsername = strings.TrimSpace(req.SASLUsername)
	row.Description = strings.TrimSpace(req.Description)
	if req.SASLPassword != "" {
		plainPass, err := h.secrets.UnwrapTransport(req.SASLPassword)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		sealed, err := h.secrets.Seal(plainPass)
		if err != nil {
			response.Internal(c, "failed to seal password")
			return
		}
		row.SASLPassword = sealed
	}
	if err := h.db.Save(row).Error; err != nil {
		response.Internal(c, "failed to update connection")
		return
	}
	response.OK(c, toDTO(*row))
}

func (h *handler) deleteConn(c *gin.Context) {
	row, ok := h.loadOwned(c)
	if !ok {
		return
	}
	if err := h.db.Delete(row).Error; err != nil {
		response.Internal(c, "failed to delete connection")
		return
	}
	response.OK(c, gin.H{"id": row.ID})
}

func (h *handler) ping(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	response.OK(c, Ping(row))
}

func (h *handler) clusterDetail(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	detail, err := FetchClusterDetail(row)
	if err != nil && !detail.Overview.Available {
		response.Fail(c, http.StatusBadGateway, 502, "failed to fetch cluster: "+err.Error())
		return
	}
	response.OK(c, detail)
}

func (h *handler) connStatus(c *gin.Context) {
	uid, _ := auth.UserID(c)
	var rows []model.KafkaConn
	if err := h.db.Where("user_id = ?", uid).Find(&rows).Error; err != nil {
		response.Internal(c, "failed to list connections")
		return
	}
	response.OK(c, summarizeConns(rows, h.openConn))
}

// testConn probes connectivity from form fields without saving.
// Optional id: when password is blank on edit, reuse the stored password.
func (h *handler) testConn(c *gin.Context) {
	uid, _ := auth.UserID(c)
	var req connRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Brokers) == "" {
		response.BadRequest(c, "brokers is required")
		return
	}
	plainPass, err := h.secrets.UnwrapTransport(req.SASLPassword)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	row := model.KafkaConn{
		Brokers:          strings.TrimSpace(req.Brokers),
		SecurityProtocol: defaultProtocol(req.SecurityProtocol),
		SASLMechanism:    strings.TrimSpace(req.SASLMechanism),
		SASLUsername:     strings.TrimSpace(req.SASLUsername),
		SASLPassword:     plainPass,
	}
	if req.ID > 0 && plainPass == "" {
		var stored model.KafkaConn
		if err := h.db.Where("id = ? AND user_id = ?", req.ID, uid).First(&stored).Error; err == nil {
			opened, err := h.openConn(&stored)
			if err != nil {
				response.Internal(c, "failed to decrypt stored password")
				return
			}
			row.SASLPassword = opened.SASLPassword
			if row.SASLUsername == "" {
				row.SASLUsername = stored.SASLUsername
			}
		}
	}
	response.OK(c, Ping(&row))
}

func (h *handler) listTopics(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	result, err := ListTopics(row, TopicListQuery{
		Q:        c.Query("q"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to list topics: "+err.Error())
		return
	}
	response.OK(c, result)
}

func (h *handler) topicDetail(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	topic := strings.TrimPrefix(c.Param("topic"), "/")
	detail, err := DescribeTopic(row, topic)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to describe topic: "+err.Error())
		return
	}
	response.OK(c, detail)
}

func (h *handler) deleteTopic(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	topic := strings.TrimPrefix(c.Param("topic"), "/")
	if topic == "" {
		response.BadRequest(c, "topic is required")
		return
	}
	if err := DeleteTopic(row, topic); err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to delete topic: "+err.Error())
		return
	}
	response.OK(c, gin.H{"topic": topic})
}

type consumeRequest struct {
	Topic     string `json:"topic"`
	Partition *int32 `json:"partition"`
	From      string `json:"from"`
	Offset    int64  `json:"offset"`
	Limit     int    `json:"limit"`
	Since     string `json:"since"`
	Until     string `json:"until"`
}

func (h *handler) consume(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	var req consumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	topic := strings.TrimSpace(req.Topic)
	if topic == "" {
		response.BadRequest(c, "topic is required")
		return
	}
	partition := int32(-1)
	if req.Partition != nil {
		partition = *req.Partition
	}
	since, err := parseConsumeTime(req.Since)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	until, err := parseConsumeTime(req.Until)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if !since.IsZero() && !until.IsZero() && !since.Before(until) {
		response.BadRequest(c, "since must be before until")
		return
	}
	msgs, err := ConsumeMessages(c.Request.Context(), row, ConsumeQuery{
		Topic:     topic,
		Partition: partition,
		From:      req.From,
		Offset:    req.Offset,
		Limit:     req.Limit,
		Since:     since,
		Until:     until,
	})
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to consume: "+err.Error())
		return
	}
	response.OK(c, msgs)
}

type produceRequest struct {
	Topic     string `json:"topic"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Partition *int32 `json:"partition"`
}

func (h *handler) produce(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	var req produceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	topic := strings.TrimSpace(req.Topic)
	if topic == "" {
		response.BadRequest(c, "topic is required")
		return
	}
	partition := int32(-1)
	if req.Partition != nil {
		partition = *req.Partition
	}
	res, err := ProduceMessage(row, topic, req.Key, req.Value, partition)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to produce: "+err.Error())
		return
	}
	response.OK(c, res)
}

func (h *handler) loadOwned(c *gin.Context) (*model.KafkaConn, bool) {
	uid, _ := auth.UserID(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.BadRequest(c, "invalid connection id")
		return nil, false
	}
	var row model.KafkaConn
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

func (h *handler) openConn(row *model.KafkaConn) (*model.KafkaConn, error) {
	cp := *row
	plain, err := h.secrets.Open(cp.SASLPassword)
	if err != nil {
		return nil, err
	}
	cp.SASLPassword = plain
	return &cp, nil
}

func (h *handler) loadOwnedForUse(c *gin.Context) (*model.KafkaConn, bool) {
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

func validateConnReq(req *connRequest, creating bool) error {
	if strings.TrimSpace(req.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(req.Brokers) == "" {
		return errors.New("brokers is required")
	}
	_ = creating
	return nil
}

func defaultProtocol(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "PLAINTEXT"
	}
	return strings.ToUpper(p)
}

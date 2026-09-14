package es

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
	ID            uint   `json:"id"`
	UserID        uint   `json:"userId"`
	Name          string `json:"name"`
	Addresses     string `json:"addresses"`
	Username      string `json:"username"`
	Description   string `json:"description"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
	Available     *bool  `json:"available,omitempty"`
	ClusterStatus string `json:"clusterStatus,omitempty"`
	ClusterName   string `json:"clusterName,omitempty"`
	Version       string `json:"version,omitempty"`
	NodeCount     int    `json:"nodeCount,omitempty"`
	StatusMessage string `json:"statusMessage,omitempty"`
}

type connRequest struct {
	ID          uint   `json:"id"` // optional; used by test to reuse stored password
	Name        string `json:"name"`
	Addresses   string `json:"addresses"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Description string `json:"description"`
}

func toDTO(c model.EsConn) connDTO {
	return connDTO{
		ID:          c.ID,
		UserID:      c.UserID,
		Name:        c.Name,
		Addresses:   c.Addresses,
		Username:    c.Username,
		Description: c.Description,
		CreatedAt:   c.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:   c.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

func applyOverview(dto *connDTO, ov ClusterOverview) {
	dto.Available = &ov.Available
	dto.ClusterStatus = ov.Status
	dto.ClusterName = ov.ClusterName
	dto.Version = ov.Version
	dto.NodeCount = ov.NumberOfNodes
	dto.StatusMessage = ov.Message
}

func (h *handler) listConns(c *gin.Context) {
	uid, _ := auth.UserID(c)
	var rows []model.EsConn
	if err := h.db.Where("user_id = ?", uid).Order("id desc").Find(&rows).Error; err != nil {
		response.Internal(c, "failed to list connections")
		return
	}
	// Default: skip probe so the list returns quickly; use ?check=1 to probe inline.
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
				applyOverview(&dto, FetchOverview(use))
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
	row := model.EsConn{
		UserID:      uid,
		Name:        strings.TrimSpace(req.Name),
		Addresses:   strings.TrimSpace(req.Addresses),
		Username:    strings.TrimSpace(req.Username),
		Password:    sealed,
		Description: strings.TrimSpace(req.Description),
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
	if err := validateConnReq(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	row.Name = strings.TrimSpace(req.Name)
	row.Addresses = strings.TrimSpace(req.Addresses)
	row.Username = strings.TrimSpace(req.Username)
	row.Description = strings.TrimSpace(req.Description)
	if req.Password != "" {
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
		row.Password = sealed
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

func (h *handler) connStatus(c *gin.Context) {
	uid, _ := auth.UserID(c)
	var rows []model.EsConn
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
	if strings.TrimSpace(req.Addresses) == "" {
		response.BadRequest(c, "addresses is required")
		return
	}
	plainPass, err := h.secrets.UnwrapTransport(req.Password)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	row := model.EsConn{
		Addresses: strings.TrimSpace(req.Addresses),
		Username:  strings.TrimSpace(req.Username),
		Password:  plainPass,
	}
	if req.ID > 0 && plainPass == "" {
		var stored model.EsConn
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

func (h *handler) listIndices(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	result, err := ListIndices(row, c.Query("q"))
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to list indices: "+err.Error())
		return
	}
	response.OK(c, result)
}

type createIndexRequest struct {
	Name     string         `json:"name"`
	Mappings map[string]any `json:"mappings"`
	Settings map[string]any `json:"settings"`
	Aliases  map[string]any `json:"aliases"`
}

func (h *handler) createIndex(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	var req createIndexRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		response.BadRequest(c, "name is required")
		return
	}
	if err := CreateIndex(row, name, req.Mappings, req.Settings, req.Aliases); err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to create index: "+err.Error())
		return
	}
	response.OK(c, gin.H{"index": name})
}

func (h *handler) indexStructure(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	index := strings.TrimSpace(c.Param("index"))
	if index == "" {
		response.BadRequest(c, "index is required")
		return
	}
	st, err := GetIndexStructure(row, index)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to get index structure: "+err.Error())
		return
	}
	response.OK(c, st)
}

func (h *handler) deleteIndex(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	index := strings.TrimSpace(c.Param("index"))
	if index == "" {
		response.BadRequest(c, "index is required")
		return
	}
	if err := DeleteIndex(row, index); err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to delete index: "+err.Error())
		return
	}
	response.OK(c, gin.H{"index": index})
}

func (h *handler) clearIndex(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	index := strings.TrimSpace(c.Param("index"))
	if index == "" {
		response.BadRequest(c, "index is required")
		return
	}
	if err := ClearIndex(row, index); err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to clear index: "+err.Error())
		return
	}
	response.OK(c, gin.H{"index": index})
}

type aliasRequest struct {
	Alias   string `json:"alias"`
	Replace bool   `json:"replace"`
}

func (h *handler) setAlias(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	index := strings.TrimSpace(c.Param("index"))
	var req aliasRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	if index == "" || strings.TrimSpace(req.Alias) == "" {
		response.BadRequest(c, "index and alias are required")
		return
	}
	if err := SetIndexAlias(row, index, req.Alias, req.Replace); err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to set alias: "+err.Error())
		return
	}
	response.OK(c, gin.H{"index": index, "alias": strings.TrimSpace(req.Alias)})
}

type cloneRequest struct {
	Target string `json:"target"`
}

func (h *handler) cloneIndex(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	index := strings.TrimSpace(c.Param("index"))
	var req cloneRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	if index == "" || strings.TrimSpace(req.Target) == "" {
		response.BadRequest(c, "index and target are required")
		return
	}
	if err := CloneIndex(row, index, req.Target); err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to clone index: "+err.Error())
		return
	}
	response.OK(c, gin.H{"index": index, "target": strings.TrimSpace(req.Target)})
}

type rebuildRequest struct {
	Mappings map[string]any `json:"mappings"`
	Settings map[string]any `json:"settings"`
}

func (h *handler) listIndexFields(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	index := strings.TrimSpace(c.Param("index"))
	if index == "" {
		response.BadRequest(c, "index is required")
		return
	}
	st, err := GetIndexStructure(row, index)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to list fields: "+err.Error())
		return
	}
	all := collectMappingFieldInfos(st.Mappings, "", true)
	writable := collectMappingFieldInfos(st.Mappings, "", false)
	fields := make([]string, 0, len(all)+1)
	fields = append(fields, "_id")
	for _, f := range all {
		fields = append(fields, f.Name)
	}
	response.OK(c, gin.H{"fields": fields, "items": writable})
}

func (h *handler) searchDocs(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	index := strings.TrimSpace(c.Param("index"))
	if index == "" {
		response.BadRequest(c, "index is required")
		return
	}
	var req DocSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		response.BadRequest(c, "invalid request body")
		return
	}
	result, err := SearchDocs(row, index, req)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to search docs: "+err.Error())
		return
	}
	response.OK(c, result)
}

func (h *handler) createDoc(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	index := strings.TrimSpace(c.Param("index"))
	if index == "" {
		response.BadRequest(c, "index is required")
		return
	}
	var req DocCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	if req.Source == nil {
		response.BadRequest(c, "source is required")
		return
	}
	result, err := CreateDoc(row, index, req)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to create doc: "+err.Error())
		return
	}
	response.OK(c, result)
}

func (h *handler) updateDoc(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	index := strings.TrimSpace(c.Param("index"))
	docID := strings.TrimSpace(c.Param("docId"))
	if index == "" {
		response.BadRequest(c, "index is required")
		return
	}
	if docID == "" {
		response.BadRequest(c, "document id is required")
		return
	}
	var req struct {
		Source map[string]any `json:"source"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	if req.Source == nil {
		response.BadRequest(c, "source is required")
		return
	}
	result, err := UpdateDoc(row, index, docID, req.Source)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to update doc: "+err.Error())
		return
	}
	response.OK(c, result)
}

func (h *handler) bulkDeleteDocs(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	index := strings.TrimSpace(c.Param("index"))
	if index == "" {
		response.BadRequest(c, "index is required")
		return
	}
	var req DocIDsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	result, err := DeleteDocs(row, index, req.IDs)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to delete docs: "+err.Error())
		return
	}
	response.OK(c, result)
}

func (h *handler) bulkUpdateDocs(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	index := strings.TrimSpace(c.Param("index"))
	if index == "" {
		response.BadRequest(c, "index is required")
		return
	}
	var req DocBulkUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	result, err := BulkUpdateDocs(row, index, req.IDs, req.Doc)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to update docs: "+err.Error())
		return
	}
	response.OK(c, result)
}

func (h *handler) rebuildIndex(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	index := strings.TrimSpace(c.Param("index"))
	if index == "" {
		response.BadRequest(c, "index is required")
		return
	}
	var req rebuildRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		response.BadRequest(c, "invalid request body")
		return
	}
	result, err := RebuildIndex(row, index, req.Mappings, req.Settings)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to rebuild index: "+err.Error())
		return
	}
	response.OK(c, result)
}

func (h *handler) loadOwned(c *gin.Context) (*model.EsConn, bool) {
	uid, _ := auth.UserID(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.BadRequest(c, "invalid connection id")
		return nil, false
	}
	var row model.EsConn
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

func (h *handler) openConn(row *model.EsConn) (*model.EsConn, error) {
	cp := *row
	plain, err := h.secrets.Open(cp.Password)
	if err != nil {
		return nil, err
	}
	cp.Password = plain
	return &cp, nil
}

func (h *handler) loadOwnedForUse(c *gin.Context) (*model.EsConn, bool) {
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
	if strings.TrimSpace(req.Addresses) == "" {
		return errors.New("addresses is required")
	}
	return nil
}

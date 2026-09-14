package redis

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hello-coder/hello-coder/internal/response"
)

func (h *handler) listDBs(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	dbs, err := ListDatabases(row)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to list databases: "+err.Error())
		return
	}
	response.OK(c, gin.H{"items": dbs, "mode": row.Mode})
}

func (h *handler) scanKeys(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	db, _ := strconv.Atoi(c.DefaultQuery("db", "0"))
	cursor := c.DefaultQuery("cursor", "0")
	count, _ := strconv.ParseInt(c.DefaultQuery("count", "20"), 10, 64)
	match := c.Query("match")
	result, err := ScanKeys(row, db, cursor, count, match)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to scan keys: "+err.Error())
		return
	}
	response.OK(c, result)
}

type keyGetRequest struct {
	DB  int    `json:"db"`
	Key string `json:"key"`
}

func (h *handler) getKey(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	var req keyGetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	entry, err := GetEntry(row, req.DB, req.Key)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to get key: "+err.Error())
		return
	}
	response.OK(c, entry)
}

type keySetRequest struct {
	DB    int    `json:"db"`
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value string `json:"value"`
	TTL   int64  `json:"ttl"`
}

func (h *handler) setKey(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	var req keySetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Key) == "" {
		response.BadRequest(c, "key is required")
		return
	}
	if err := SetEntry(row, req.DB, req.Key, req.Type, req.Value, req.TTL); err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to set key: "+err.Error())
		return
	}
	response.OK(c, gin.H{"key": strings.TrimSpace(req.Key)})
}

type keyExpireRequest struct {
	DB  int    `json:"db"`
	Key string `json:"key"`
	TTL int64  `json:"ttl"` // seconds; <=0 = forever (PERSIST)
}

func (h *handler) expireKey(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	var req keyExpireRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Key) == "" {
		response.BadRequest(c, "key is required")
		return
	}
	if err := SetKeyTTL(row, req.DB, req.Key, req.TTL); err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to set ttl: "+err.Error())
		return
	}
	response.OK(c, gin.H{"key": strings.TrimSpace(req.Key), "ttl": req.TTL})
}

type keyDelRequest struct {
	DB   int      `json:"db"`
	Keys []string `json:"keys"`
}

func (h *handler) deleteKeys(c *gin.Context) {
	row, ok := h.loadOwnedForUse(c)
	if !ok {
		return
	}
	var req keyDelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	n, err := DeleteEntries(row, req.DB, req.Keys)
	if err != nil {
		response.Fail(c, http.StatusBadGateway, 502, "failed to delete keys: "+err.Error())
		return
	}
	response.OK(c, gin.H{"deleted": n})
}

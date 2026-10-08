package controller

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/workbuddy"
	"github.com/gin-gonic/gin"
)

func GetWorkBuddyChannelState(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"success": false, "message": "Invalid channel ID"})
		return
	}
	channel, err := model.GetChannelById(id, true)
	if err != nil {
		c.JSON(404, gin.H{"success": false, "message": "Channel not found"})
		return
	}
	pool, err := workbuddy.ForChannel(channel)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"success": true, "data": pool.Status()})
}
func UpdateWorkBuddyChannelSettings(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"success": false, "message": "Invalid channel ID"})
		return
	}
	channel, err := model.GetChannelById(id, true)
	if err != nil {
		c.JSON(404, gin.H{"success": false, "message": "Channel not found"})
		return
	}
	pool, err := workbuddy.ForChannel(channel)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "message": err.Error()})
		return
	}
	var request struct {
		Distribution *string `json:"distribution"`
		AutoCheckin  *bool   `json:"autoCheckin"`
		Index        *int    `json:"index"`
		Enabled      *bool   `json:"enabled"`
	}
	if err = c.ShouldBindJSON(&request); err != nil {
		c.JSON(400, gin.H{"success": false, "message": "Invalid settings"})
		return
	}
	if err = pool.Settings(request.Distribution, request.AutoCheckin, request.Index, request.Enabled); err != nil {
		c.JSON(400, gin.H{"success": false, "message": err.Error()})
		return
	}
	recordManageAudit(c, "channel.workbuddy.settings", map[string]any{"channel_id": id, "account_index": request.Index, "enabled": request.Enabled, "auto_checkin": request.AutoCheckin, "distribution": request.Distribution})
	c.JSON(200, gin.H{"success": true, "data": pool.Status()})
}
func WorkBuddyChannelAccountAction(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"success": false, "message": "Invalid channel ID"})
		return
	}
	index, err := strconv.Atoi(c.Param("index"))
	if err != nil {
		c.JSON(400, gin.H{"success": false, "message": "Invalid account index"})
		return
	}
	channel, err := model.GetChannelById(id, true)
	if err != nil {
		c.JSON(404, gin.H{"success": false, "message": "Channel not found"})
		return
	}
	pool, err := workbuddy.ForChannel(channel)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	result, err := pool.AccountAction(ctx, index, c.Param("action"))
	if err != nil {
		c.JSON(400, gin.H{"success": false, "message": err.Error()})
		return
	}
	recordManageAudit(c, "channel.workbuddy."+c.Param("action"), map[string]any{"channel_id": id, "account_index": index})
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"success": true, "data": result})
}

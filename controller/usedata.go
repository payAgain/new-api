package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func parseFlowQuotaTimeRange(c *gin.Context) (int64, int64, bool) {
	startTimestamp, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || startTimestamp <= 0 {
		common.ApiErrorMsg(c, "invalid start_timestamp")
		return 0, 0, false
	}
	endTimestamp, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || endTimestamp <= 0 {
		common.ApiErrorMsg(c, "invalid end_timestamp")
		return 0, 0, false
	}
	if endTimestamp < startTimestamp {
		common.ApiErrorMsg(c, "invalid time range")
		return 0, 0, false
	}
	return startTimestamp, endTimestamp, true
}

func GetAllQuotaDates(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	dates, err := model.GetAllQuotaDates(startTimestamp, endTimestamp, username)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

func GetQuotaDatesByUser(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	dates, err := model.GetQuotaDataGroupByUser(startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
}

func GetUserQuotaDates(c *gin.Context) {
	userId := c.GetInt("id")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	// 判断时间跨度是否超过 1 个月
	if endTimestamp-startTimestamp > 2592000 {
		common.ApiErrorT(c, "The time range cannot exceed 1 month")
		return
	}
	dates, err := model.GetQuotaDataByUserId(userId, startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

func GetAllFlowQuotaDates(c *gin.Context) {
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	username := c.Query("username")
	dates, err := model.GetFlowQuotaData(startTimestamp, endTimestamp, username, 0, c.GetInt("role"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

func GetUserFlowQuotaDates(c *gin.Context) {
	userId := c.GetInt("id")
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	if endTimestamp-startTimestamp > 2592000 {
		common.ApiErrorT(c, "The time range cannot exceed 1 month")
		return
	}
	dates, err := model.GetFlowQuotaData(startTimestamp, endTimestamp, "", userId, common.RoleCommonUser)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

func GetAllUsageAnalysis(c *gin.Context) {
	if c.GetInt("role") < common.RoleAdminUser {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": "insufficient permission"})
		return
	}
	getUsageAnalysis(c, false)
}

func GetSelfUsageAnalysis(c *gin.Context) {
	if c.GetInt("id") <= 0 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
		return
	}
	getUsageAnalysis(c, true)
}

func getUsageAnalysis(c *gin.Context, self bool) {
	start, startErr := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	end, endErr := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if startErr != nil || endErr != nil {
		common.ApiErrorMsg(c, model.ErrUsageTimeRange.Error())
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("timezone_offset", "0"))
	if err != nil {
		common.ApiErrorMsg(c, model.ErrUsageOptions.Error())
		return
	}
	query := model.UsageAnalysisQuery{
		StartTimestamp: start, EndTimestamp: end,
		Granularity: c.DefaultQuery("granularity", "hour"), TimezoneOffset: offset,
		ModelName: c.Query("model_name"),
	}
	if self {
		// User-supplied user_id/username cannot widen a self query.
		query.UserID = c.GetInt("id")
	} else {
		query.Username = c.Query("username")
	}
	var data any
	switch c.DefaultQuery("view", "analysis") {
	case "analysis":
		data, err = model.GetUsageAnalysis(c.Request.Context(), query)
	case "models":
		data, err = model.GetUsageModels(c.Request.Context(), query)
	case "requests":
		page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, sizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		if pageErr != nil || sizeErr != nil {
			common.ApiErrorMsg(c, model.ErrUsageOptions.Error())
			return
		}
		data, err = model.GetUsageRequests(c.Request.Context(), query, page, pageSize)
	default:
		common.ApiErrorMsg(c, model.ErrUsageOptions.Error())
		return
	}
	if err != nil {
		if errors.Is(err, model.ErrUsageTimeRange) || errors.Is(err, model.ErrUsageOptions) || errors.Is(err, model.ErrUsageTooLarge) || errors.Is(err, model.ErrUsageOverflow) {
			common.ApiError(c, err)
			return
		}
		common.SysError("failed to query usage analysis: " + err.Error())
		common.ApiErrorMsg(c, "failed to query usage data")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}

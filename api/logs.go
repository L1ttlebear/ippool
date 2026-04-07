package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/database/models"
)

// GetLogs returns paginated audit logs with optional type filter.
func GetLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	msgType := c.Query("type")

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := (page - 1) * limit

	db := dbcore.GetDBInstance()
	query := db.Model(&models.Log{}).Order("id DESC")
	if msgType != "" {
		query = query.Where("msg_type = ?", msgType)
	}

	var total int64
	query.Count(&total)

	var logs []models.Log
	if err := query.Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total": total,
		"page":  page,
		"limit": limit,
		"data":  logs,
	})
}

// GetRecentLogs returns the 20 most recent log entries.
func GetRecentLogs(c *gin.Context) {
	db := dbcore.GetDBInstance()
	var logs []models.Log
	if err := db.Order("id DESC").Limit(20).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, logs)
}

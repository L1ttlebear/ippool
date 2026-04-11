package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/database/models"
	"github.com/gin-gonic/gin"
)

var poolNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)

func GetPools(c *gin.Context) {
	db := dbcore.GetDBInstance()
	var pools []models.Pool
	if err := db.Order("name asc").Find(&pools).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	type item struct {
		Name      string `json:"name"`
		HostCount int64  `json:"host_count"`
	}
	resp := make([]item, 0, len(pools))
	for _, p := range pools {
		var cnt int64
		_ = db.Model(&models.Host{}).Where("pool = ?", p.Name).Count(&cnt).Error
		resp = append(resp, item{Name: p.Name, HostCount: cnt})
	}
	c.JSON(http.StatusOK, resp)
}

func CreatePool(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if !poolNameRegexp.MatchString(name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pool name must match [a-zA-Z0-9_-], length 1-100"})
		return
	}
	db := dbcore.GetDBInstance()
	var cnt int64
	if err := db.Model(&models.Pool{}).Where("name = ?", name).Count(&cnt).Error; err == nil && cnt > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "pool already exists"})
		return
	}
	p := models.Pool{Name: name}
	if err := db.Create(&p).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, p)
}

func DeletePool(c *gin.Context) {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pool name"})
		return
	}
	if name == "default" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "default pool cannot be deleted"})
		return
	}
	db := dbcore.GetDBInstance()
	var cnt int64
	if err := db.Model(&models.Host{}).Where("pool = ?", name).Count(&cnt).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if cnt > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pool has hosts, cannot delete"})
		return
	}
	if err := db.Where("name = ?", name).Delete(&models.Pool{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

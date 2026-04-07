package auditlog

import (
	"log"
	"time"

	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/database/models"
)

func Log(ip, uuid, message, msgType string) {
	now := time.Now()
	db := dbcore.GetDBInstance()
	logEntry := &models.Log{
		IP:      ip,
		UUID:    uuid,
		Message: message,
		MsgType: msgType,
		Time:    models.FromTime(now),
	}
	db.Create(logEntry)
}

func EventLog(eventType, message string) {
	Log("", "", message, eventType)
}

// RemoveOldLogs deletes logs older than 30 days.
func RemoveOldLogs() {
	CleanOldLogs(30)
}

// CleanOldLogs removes logs older than the specified number of days.
func CleanOldLogs(days int) {
	db := dbcore.GetDBInstance()
	threshold := time.Now().AddDate(0, 0, -days)
	if err := db.Where("time < ?", threshold).Delete(&models.Log{}).Error; err != nil {
		log.Println("Failed to clean old logs:", err)
	}
}

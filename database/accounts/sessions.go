package accounts

import (
	"errors"
	"time"

	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/database/models"
	"github.com/L1ttlebear/ippool/utils"
)

func GetAllSessions() (sessions []models.Session, err error) {
	db := dbcore.GetDBInstance()
	err = db.Find(&sessions).Error
	return sessions, err
}

func CreateSession(uuid string, expires int, userAgent, ip, loginMethod string) (string, error) {
	db := dbcore.GetDBInstance()
	session := utils.GenerateRandomString(32)
	sessionRecord := models.Session{
		UUID:         uuid,
		Session:      session,
		Expires:      models.FromTime(time.Now().Add(time.Duration(expires) * time.Second)),
		UserAgent:    userAgent,
		Ip:           ip,
		LoginMethod:  loginMethod,
		LatestOnline: models.FromTime(time.Now()),
	}
	err := db.Create(&sessionRecord).Error
	if err != nil {
		return "", err
	}
	return session, nil
}

func GetSession(session string) (uuid string, err error) {
	db := dbcore.GetDBInstance()
	var sessionRecord models.Session
	err = db.Where("session = ?", session).First(&sessionRecord).Error
	if err != nil {
		return "", err
	}
	if time.Now().After(sessionRecord.Expires.ToTime()) {
		_ = DeleteSession(session)
		return "", errors.New("session expired")
	}
	return sessionRecord.UUID, nil
}

func GetUserBySession(session string) (models.User, error) {
	db := dbcore.GetDBInstance()
	var sessionRecord models.Session
	err := db.Where("session = ?", session).First(&sessionRecord).Error
	if err != nil {
		return models.User{}, err
	}
	return GetUserByUUID(sessionRecord.UUID)
}

func DeleteSession(session string) error {
	db := dbcore.GetDBInstance()
	return db.Where("session = ?", session).Delete(&models.Session{}).Error
}

func DeleteAllSessions() error {
	db := dbcore.GetDBInstance()
	return db.Where("1 = 1").Delete(&models.Session{}).Error
}

func UpdateLatest(session, useragent, ip string) error {
	db := dbcore.GetDBInstance()
	return db.Model(&models.Session{}).Where("session = ?", session).Updates(map[string]interface{}{
		"latest_online":     time.Now(),
		"latest_user_agent": useragent,
		"latest_ip":         ip,
	}).Error
}

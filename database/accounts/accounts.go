package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/database/models"
	"github.com/L1ttlebear/ippool/utils"
)

const constantSalt = "06Wm4Jv1Hkxx"

func CheckPassword(username, passwd string) (string, bool) {
	db := dbcore.GetDBInstance()
	var user models.User
	if err := db.Where("username = ?", username).First(&user).Error; err != nil {
		return "", false
	}
	if hashPasswd(passwd) != user.Passwd {
		return "", false
	}
	return user.UUID, true
}

func ForceResetPassword(username, passwd string) error {
	db := dbcore.GetDBInstance()
	result := db.Model(&models.User{}).Where("username = ?", username).Update("passwd", hashPasswd(passwd))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user not found: %s", username)
	}
	return nil
}

func hashPasswd(passwd string) string {
	hash := sha256.New()
	hash.Write([]byte(passwd + constantSalt))
	return base64.StdEncoding.EncodeToString(hash.Sum(nil))
}

func CreateDefaultAdminAccount() (username, passwd string, err error) {
	db := dbcore.GetDBInstance()
	username = os.Getenv("ADMIN_USERNAME")
	if username == "" {
		username = "admin"
	}
	passwd = os.Getenv("ADMIN_PASSWORD")
	if passwd == "" {
		passwd = utils.GeneratePassword()
	}
	user := models.User{
		UUID:      uuid.New().String(),
		Username:  username,
		Passwd:    hashPasswd(passwd),
		CreatedAt: models.FromTime(time.Now()),
		UpdatedAt: models.FromTime(time.Now()),
	}
	err = db.Create(&user).Error
	if err != nil {
		return "", "", err
	}
	return username, passwd, nil
}

func GetUserByUUID(id string) (models.User, error) {
	db := dbcore.GetDBInstance()
	var user models.User
	err := db.Where("uuid = ?", id).First(&user).Error
	return user, err
}

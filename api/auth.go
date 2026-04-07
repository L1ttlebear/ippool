package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/L1ttlebear/ippool/database/accounts"
	"github.com/L1ttlebear/ippool/web"
)

// GetLogin renders the login page. Redirects to / if already logged in.
func GetLogin(c *gin.Context) {
	if cookie, err := c.Cookie("session_token"); err == nil && cookie != "" {
		if _, err := accounts.GetSession(cookie); err == nil {
			c.Redirect(http.StatusFound, "/")
			return
		}
	}
	web.RenderLogin(c, web.LoginPageData{})
}

// PostLogin validates credentials and sets a session cookie on success.
func PostLogin(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	uuid, ok := accounts.CheckPassword(username, password)
	if !ok {
		web.RenderLogin(c, web.LoginPageData{Error: "用户名或密码错误"})
		return
	}

	session, err := accounts.CreateSession(uuid, 86400*30, c.Request.UserAgent(), c.ClientIP(), "password")
	if err != nil {
		web.RenderLogin(c, web.LoginPageData{Error: "创建会话失败，请重试"})
		return
	}

	c.SetCookie("session_token", session, 86400*30, "/", "", false, true)
	c.Redirect(http.StatusFound, "/")
}

// GetLogout clears the session and redirects to /login.
func GetLogout(c *gin.Context) {
	if cookie, err := c.Cookie("session_token"); err == nil {
		_ = accounts.DeleteSession(cookie)
	}
	c.SetCookie("session_token", "", -1, "/", "", false, true)
	c.SetSameSite(http.SameSiteLaxMode)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:    "session_token",
		Value:   "",
		Path:    "/",
		Expires: time.Unix(0, 0),
		MaxAge:  -1,
	})
	c.Redirect(http.StatusFound, "/login")
}

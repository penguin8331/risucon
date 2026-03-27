package main

// sqlx については https://jmoiron.github.io/sqlx/ を参照

import (
	// "fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"

	"github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	echolog "github.com/labstack/gommon/log"
)

const (
	listenPort           = 8080
	frontendContentsPath = "../public"
)

var (
	dbConn *sqlx.DB
	secret = []byte("risucon_session_cookiestore_defaultsecret")
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	if secretKey, ok := os.LookupEnv("RISUCON_SESSION_SECRETKEY"); ok {
		secret = []byte(secretKey)
	}
}

type InitializeResponse struct {
	Language string `json:"language"`
}

// 環境変数を取得する、なければデフォルト値を返す
func getEnv(key string, defaultValue string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return defaultValue
}

// DBに接続する
func connectDB() (*sqlx.DB, error) {
	config := mysql.NewConfig()
	config.Net = "tcp"
	config.Addr = getEnv("RISUCON_DB_HOST", "127.0.0.1") + ":" + getEnv("RISUCON_DB_PORT", "3306")
	config.User = getEnv("RISUCON_DB_USER", "risucon")
	config.Passwd = getEnv("RISUCON_DB_PASSWORD", "risucon")
	config.DBName = getEnv("RISUCON_DB_NAME", "risucontest")
	config.ParseTime = true
	dsn := config.FormatDSN()
	return sqlx.Open("mysql", dsn)
}

func initializeHandler(c echo.Context) error {
	if out, err := exec.Command("../sql/init.sh").CombinedOutput(); err != nil {
		c.Logger().Warnf("init.sh failed with err=%s", string(out))
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to initialize: "+err.Error())
	}

	// キャッシュを消す
	subtaskcache = sync.Map{}

	c.Request().Header.Add("Content-Type", "application/json;charset=utf-8")
	return c.JSON(http.StatusOK, InitializeResponse{
		Language: "golang",
	})
}

func main() {
	e := echo.New()
	e.Debug = true
	e.Logger.SetLevel(echolog.DEBUG)
	e.Use(middleware.Logger())
	cookiestore := sessions.NewCookieStore(secret)
	e.Use(session.Middleware(cookiestore))

	// 初期化
	e.POST("/api/initialize", initializeHandler)

	// user
	e.POST("/api/register", registerHandler)
	e.POST("/api/login", loginHandler)
	e.POST("/api/logout", logoutHandler)
	e.GET("/api/user/:username", getUserHandler)

	// team
	e.POST("/api/team/create", createTeamHandler)
	e.POST("/api/team/join", joinTeamHandler)
	e.GET("/api/team/:teamname", getTeamHandler)

	// contest
	e.GET("/api/tasks", getTasksHandler)
	e.GET("/api/standings", getStandingsHandler)
	e.GET("/api/tasks/:taskname", getTaskHandler)
	e.POST("/api/submit", submitHandler)
	e.GET("/api/submissions", getSubmissionsHandler)

	// for admin
	e.POST("/api/admin/createtask", createTaskHandler)

	// 静的ファイル
	e.Static("/assets", frontendContentsPath+"/assets")

	// 以上に当てはまらなければ index.html を返す
	e.GET("/*", getIndexHandler)
	

	// DB接続
	db, err := connectDB()
	if err != nil {
		e.Logger.Errorf("failed to connect db: %v", err)
		os.Exit(1)
	}
	db.SetMaxOpenConns(10)
	defer db.Close()
	if err := db.Ping(); err != nil {
		e.Logger.Errorf("failed to ping db: %v", err)
		os.Exit(1)
	}
	dbConn = db

	// サーバー起動
	listenAddr := net.JoinHostPort("", strconv.Itoa(listenPort))
	e.Logger.Infof("listening on %s", listenAddr)
	if err := e.Start(listenAddr); err != nil {
		e.Logger.Errorf("failed to start server: %v", err)
		os.Exit(1)
	}
}

func getIndexHandler(c echo.Context) error {
	return c.File(frontendContentsPath + "/index.html")
}

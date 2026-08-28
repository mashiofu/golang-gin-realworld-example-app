package main

import (
	"log"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gothinkster/golang-gin-realworld-example-app/articles"
	"github.com/gothinkster/golang-gin-realworld-example-app/cache"
	"github.com/gothinkster/golang-gin-realworld-example-app/common"
	"github.com/gothinkster/golang-gin-realworld-example-app/metrics"
	"github.com/gothinkster/golang-gin-realworld-example-app/users"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) {
	users.AutoMigrate()
	db.AutoMigrate(&articles.ArticleModel{})
	db.AutoMigrate(&articles.TagModel{})
	db.AutoMigrate(&articles.FavoriteModel{})
	db.AutoMigrate(&articles.ArticleUserModel{})
	db.AutoMigrate(&articles.CommentModel{})
}

func main() {

	db := common.Init()
	Migrate(db)
	sqlDB, err := db.DB()
	if err != nil {
		log.Println("failed to get sql.DB:", err)
	} else {
		defer sqlDB.Close()
	}

	cache.Init()

	r := gin.Default()
	r.Use(metrics.Middleware())

	// Disable automatic redirect for trailing slashes
	// This prevents POST body from being lost during redirects
	r.RedirectTrailingSlash = false

	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	v1 := r.Group("/api")
	users.UsersRegister(v1.Group("/users"))
	v1.Use(users.AuthMiddleware(false))

	// Used by the cache middleware to tell "anonymous" and "authenticated"
	// requests apart - AuthMiddleware(false) above has already populated
	// my_user_model on the context by the time this runs.
	isAuthenticated := func(c *gin.Context) bool {
		return c.MustGet("my_user_model").(users.UserModel).ID != 0
	}

	// Article/profile responses are personalized per-viewer (favorited,
	// following), so they're only cached for anonymous requests. /tags has
	// no personalization at all, so it's cached for everyone.
	articles.ArticlesAnonymousRegister(v1.Group("/articles", cache.Middleware(30*time.Second, true, isAuthenticated)))
	articles.TagsAnonymousRegister(v1.Group("/tags", cache.Middleware(5*time.Minute, false, isAuthenticated)))
	users.ProfileRetrieveRegister(v1.Group("/profiles", cache.Middleware(30*time.Second, true, isAuthenticated)))

	v1.Use(users.AuthMiddleware(true))
	users.UserRegister(v1.Group("/user"))
	users.ProfileRegister(v1.Group("/profiles"))

	articles.ArticlesRegister(v1.Group("/articles"))

	testAuth := r.Group("/api/ping")

	testAuth.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	// Get port from environment variable or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if err := r.Run(":" + port); err != nil {
		log.Fatal("failed to start server:", err)
	}
}

package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Yiling-J/piper"
	"github.com/go-redis/redis/v8"
	"github.com/labstack/echo/v4"
	_ "github.com/lib/pq"
	"github.com/mattn/echo-ent-example/cacheme"
	"github.com/mattn/echo-ent-example/cacheme/fetcher"
	"github.com/mattn/echo-ent-example/config"
	"github.com/mattn/echo-ent-example/ent"
	"github.com/mattn/echo-ent-example/ent/comment"
)

func setupEcho() *echo.Echo {
	e := echo.New()
	e.Debug = true
	e.Logger.SetOutput(os.Stderr)
	return e
}

// Error indicate response erorr
type Error struct {
	Error string `json:"error"`
}

// Controller is a controller for this application.
type Controller struct {
	client  *ent.Client
	cacheme *cacheme.Client
}

// GetComment is GET handler to return record.
func (controller *Controller) GetComment(c echo.Context) error {
	// fetch record specified by parameter id
	_, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.Logger().Error("ParseInt: ", err)
		return c.String(http.StatusBadRequest, "ParseInt: "+err.Error())
	}
	comment, err := controller.cacheme.CommentCacheStore.Get(
		c.Request().Context(), c.Param("id"),
	)
	if err != nil {
		if !ent.IsNotFound(err) {
			c.Logger().Error("Get: ", err)
			return c.String(http.StatusBadRequest, "Get: "+err.Error())
		}
		return c.String(http.StatusNotFound, "Not Found")
	}
	return c.JSON(http.StatusOK, comment)
}

// ListComments is GET handler to return records.
func (controller *Controller) ListComments(c echo.Context) error {
	// fetch last 10 records
	ids, err := controller.cacheme.CommentListIDSCacheStore.Get(c.Request().Context())
	if err != nil {
		c.Logger().Error("All: ", err)
		return c.String(http.StatusBadRequest, "All: "+err.Error())
	}

	getter := controller.cacheme.CommentCacheStore.MGetter()
	for _, id := range ids {
		getter.GetM(strconv.Itoa(id))
	}
	qs, err := getter.Do(c.Request().Context())
	if err != nil {
		c.Logger().Error("All: ", err)
		return c.String(http.StatusBadRequest, "All: "+err.Error())
	}
	comments := qs.GetSlice()

	return c.JSON(http.StatusOK, comments)
}

// InsertComment is POST handler to insert record.
func (controller *Controller) InsertComment(c echo.Context) error {
	var comment ent.Comment
	// bind request to comment struct
	if err := c.Bind(&comment); err != nil {
		c.Logger().Error("Bind: ", err)
		return c.String(http.StatusBadRequest, "Bind: "+err.Error())
	}
	// insert record
	cc := controller.client.Comment.Create().SetText(comment.Text)
	if comment.Name != "" {
		cc.SetName(comment.Name)
	}
	newComment, err := cc.Save(context.Background())
	if err != nil {
		c.Logger().Error("Insert: ", err)
		return c.String(http.StatusBadRequest, "Save: "+err.Error())
	}
	c.Logger().Infof("inserted comment: %v", newComment.ID)
	err = controller.cacheme.CommentListIDSCacheStore.Invalid(c.Request().Context())
	if err != nil {
		c.Logger().Error("Update cache: ", err)
		return c.String(http.StatusBadRequest, "Update cache: "+err.Error())
	}
	return c.NoContent(http.StatusCreated)
}

func loopInsert(c *Controller) {
	var counter int
	last, err := c.client.Comment.Query().Order(ent.Desc(comment.FieldID)).First(context.TODO())
	if err != nil {
		counter = 0
	} else {
		counter = last.ID
	}
	e := echo.New()
	for {
		commentJSON := fmt.Sprintf(`{"name":"foo%d","text":"bar%d"}`, counter, counter)
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(commentJSON))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		err = c.InsertComment(ctx)
		if err != nil {
			panic(err)
		}
		counter++
		time.Sleep(1 * time.Second)
	}
}

//go:embed config/*
var configFS embed.FS

func main() {
	// load config first
	piper.SetFS(configFS)
	piper.Load("config/dev.toml")

	// setup cacheme fetcher
	fetcher.Setup()
	client, err := ent.Open("postgres", piper.IGetString(config.Database.Pg.Dsn))
	if err != nil {
		log.Fatalf("failed opening connection to postgres: %v", err)
	}
	cm := cacheme.New(redis.NewClient(&redis.Options{
		Addr: piper.IGetString(config.Database.Redis.Address),
	}))
	defer client.Close()

	// Run the auto migration tool.
	if err := client.Schema.Create(context.Background()); err != nil {
		log.Fatalf("failed creating schema resources: %v", err)
	}
	controller := &Controller{client: client, cacheme: cm}

	e := setupEcho()

	if piper.IGetBool(config.Comment.Autoinsert) {
		fmt.Println("start auto comment insert")
		go loopInsert(controller)
	}

	e.GET("/api/comments/:id", controller.GetComment)
	e.GET("/api/comments", controller.ListComments)
	e.POST("/api/comments", controller.InsertComment)
	e.Static("/", "static/")
	e.Logger.Fatal(e.Start(":8989"))
}

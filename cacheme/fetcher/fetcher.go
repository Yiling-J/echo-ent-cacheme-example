package fetcher

import (
	"context"
	"strconv"

	"github.com/Yiling-J/piper"
	"github.com/mattn/echo-ent-example/cacheme/store"
	"github.com/mattn/echo-ent-example/config"
	"github.com/mattn/echo-ent-example/ent"
	"github.com/mattn/echo-ent-example/ent/comment"
)

func Setup() {

	client, err := ent.Open("postgres", piper.IGetString(config.Database.Pg.Dsn))
	if err != nil {
		panic(err)
	}
	store.CommentCacheStore.Fetch = func(ctx context.Context, ID string) (*ent.Comment, error) {
		nid, err := strconv.Atoi(ID)
		if err != nil {
			return nil, err
		}
		return client.Comment.Get(ctx, nid)
	}
	store.CommentListIDSCacheStore.Fetch = func(ctx context.Context) ([]int, error) {
		return client.Comment.Query().Order(ent.Desc(comment.FieldCreated)).Limit(10).IDs(ctx)
	}
}

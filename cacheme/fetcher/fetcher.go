package fetcher

import (
	"context"
	"os"
	"strconv"

	"github.com/mattn/echo-ent-example/cacheme"
	"github.com/mattn/echo-ent-example/ent"
	"github.com/mattn/echo-ent-example/ent/comment"
)

func Setup() {

	client, err := ent.Open("postgres", os.Getenv("DSN"))
	if err != nil {
		panic(err)
	}
	cacheme.CommentCacheStore.Fetch = func(ctx context.Context, ID string) (*ent.Comment, error) {
		nid, err := strconv.Atoi(ID)
		if err != nil {
			return nil, err
		}
		return client.Comment.Get(ctx, nid)
	}
	cacheme.CommentListIDSCacheStore.Fetch = func(ctx context.Context) ([]int, error) {
		return client.Comment.Query().Order(ent.Desc(comment.FieldCreated)).Limit(10).IDs(ctx)
	}
}

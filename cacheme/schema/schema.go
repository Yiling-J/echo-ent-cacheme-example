//nolint
package schema

import (
	"time"

	cacheme "github.com/Yiling-J/cacheme-go"
	"github.com/mattn/echo-ent-example/ent"
)

var (
	// default prefix for redis keys
	Prefix = "cacheme"

	// store templates
	Stores = []*cacheme.StoreSchema{
		{
			Name:         "Comment",
			Key:          "comment:{{.ID}}",
			To:           &ent.Comment{},
			Version:      1,
			TTL:          6000 * time.Minute,
			Singleflight: true,
		},
		{
			Name:         "CommentListIDS",
			Key:          "commentids",
			To:           []int{},
			Version:      1,
			TTL:          6000 * time.Minute,
			Singleflight: true,
		},
	}
)

//nolint
package cacheme

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	cacheme "github.com/Yiling-J/cacheme-go"
	"github.com/go-redis/redis/v8"

	"github.com/mattn/echo-ent-example/cacheme/schema"

	"strconv"

	"github.com/mattn/echo-ent-example/ent"
)

const (
	Hit   = "HIT"
	Miss  = "MISS"
	Fetch = "FETCH"
)

func init() {

	CommentCacheStore.version = strconv.Itoa(schema.Stores[0].Version.(int))

	CommentListIDSCacheStore.version = strconv.Itoa(schema.Stores[1].Version.(int))

}

type Client struct {
	CommentCacheStore *commentCache

	CommentListIDSCacheStore *commentListIDSCache

	redis   cacheme.RedisClient
	cluster bool
	logger  cacheme.Logger
}

func (c *Client) Redis() cacheme.RedisClient {
	return c.redis
}

func (c *Client) SetLogger(l cacheme.Logger) {
	c.logger = l
}

func New(redis cacheme.RedisClient) *Client {
	client := &Client{redis: redis}

	client.CommentCacheStore = CommentCacheStore.Clone(client.redis)
	client.CommentCacheStore.SetClient(client)

	client.CommentListIDSCacheStore = CommentListIDSCacheStore.Clone(client.redis)
	client.CommentListIDSCacheStore.SetClient(client)

	client.logger = &cacheme.NOPLogger{}
	return client
}

func NewCluster(redis cacheme.RedisClient) *Client {
	client := &Client{redis: redis, cluster: true}

	client.CommentCacheStore = CommentCacheStore.Clone(client.redis)
	client.CommentCacheStore.SetClient(client)

	client.CommentListIDSCacheStore = CommentListIDSCacheStore.Clone(client.redis)
	client.CommentListIDSCacheStore.SetClient(client)

	client.logger = &cacheme.NOPLogger{}
	return client
}

func (c *Client) NewPipeline() *cacheme.CachePipeline {
	return cacheme.NewPipeline(c.redis)

}

type commentCache struct {
	Fetch       func(ctx context.Context, ID string) (*ent.Comment, error)
	tag         string
	memo        *cacheme.RedisMemoLock
	client      *Client
	version     string
	versionFunc func() string
}

type CommentPromise struct {
	executed     chan bool
	redisPromise *redis.StringCmd
	result       *ent.Comment
	error        error
	store        *commentCache
	ctx          context.Context
}

func (p *CommentPromise) WaitExecute(cp *cacheme.CachePipeline, key string, ID string) {
	defer cp.Wg.Done()
	var t *ent.Comment
	memo := p.store.memo

	<-cp.Executed
	value, err := p.redisPromise.Bytes()
	if err == nil {
		p.store.client.logger.Log(p.store.tag, key, Hit)
		err = cacheme.Unmarshal(value, &t)
		p.result, p.error = t, err
		return
	}

	resourceLock, err := memo.Lock(p.ctx, key)
	if err != nil {
		p.error = err
		return
	}
	p.store.client.logger.Log(p.store.tag, key, Miss)

	if resourceLock {
		p.store.client.logger.Log(p.store.tag, key, Fetch)
		value, err := p.store.Fetch(
			p.ctx,
			ID)
		if err != nil {
			p.error = err
			return
		}
		p.result = value
		packed, err := cacheme.Marshal(value)
		if err == nil {
			memo.SetCache(p.ctx, key, packed, time.Millisecond*360000000)
			memo.AddGroup(p.ctx, p.store.Group(), key)
		}
		p.error = err
		return
	}

	var res []byte
	if true {
		res, err = memo.WaitSingle(p.ctx, key)
	} else {
		res, err = memo.Wait(p.ctx, key)
	}
	if err == nil {
		err = cacheme.Unmarshal(res, &t)
	}
	p.result, p.error = t, err
}

func (p *CommentPromise) Result() (*ent.Comment, error) {
	return p.result, p.error
}

var CommentCacheStore = &commentCache{tag: "Comment"}

func (s *commentCache) SetClient(c *Client) {
	s.client = c
}

func (s *commentCache) Clone(r cacheme.RedisClient) *commentCache {
	value := *s
	new := &value
	lock, err := cacheme.NewRedisMemoLock(
		context.TODO(), "cacheme", r, s.tag, 5*time.Second,
	)
	if err != nil {
		fmt.Println(err)
	}
	new.memo = lock

	return new
}

func (s *commentCache) Version() string {
	if s.versionFunc != nil {
		return s.versionFunc()
	}
	return s.version
}

func (s *commentCache) KeyTemplate() string {
	return "comment:{{.ID}}" + ":v" + s.Version()
}

func (s *commentCache) Key(p *commentParam) (string, error) {
	t := template.Must(template.New("").Parse(s.KeyTemplate()))
	t = t.Option("missingkey=zero")
	var tpl bytes.Buffer
	err := t.Execute(&tpl, p)
	return tpl.String(), err
}

func (s *commentCache) Group() string {
	return "cacheme" + ":group:" + s.tag + ":v" + s.Version()
}

func (s *commentCache) versionedGroup(v string) string {
	return "cacheme" + ":group:" + s.tag + ":v" + v
}

func (s *commentCache) AddMemoLock() error {
	lock, err := cacheme.NewRedisMemoLock(context.TODO(), "cacheme", s.client.redis, s.tag, 5*time.Second)
	if err != nil {
		return err
	}

	s.memo = lock
	return nil
}

func (s *commentCache) Initialized() bool {
	return s.Fetch != nil
}

func (s *commentCache) Tag() string {
	return s.tag
}

func (s *commentCache) GetP(ctx context.Context, pp *cacheme.CachePipeline, ID string) (*CommentPromise, error) {
	param := &commentParam{}

	param.ID = ID

	key, err := s.Key(param)
	if err != nil {
		return nil, err
	}

	cacheme := s.memo

	promise := &CommentPromise{
		executed: pp.Executed,
		ctx:      ctx,
		store:    s,
	}

	wait := cacheme.GetCachedP(ctx, pp.Pipeline, key)
	promise.redisPromise = wait
	pp.Wg.Add(1)
	go promise.WaitExecute(
		pp, key, ID)
	return promise, nil
}

func (s *commentCache) Get(ctx context.Context, ID string) (*ent.Comment, error) {

	param := &commentParam{}

	param.ID = ID

	var t *ent.Comment

	key, err := s.Key(param)
	if err != nil {
		return t, err
	}

	if true {
		data, err, _ := s.memo.SingleGroup().Do(key, func() (interface{}, error) {
			return s.get(ctx, ID)
		})
		return data.(*ent.Comment), err
	}
	return s.get(ctx, ID)
}

type commentParam struct {
	ID string
}

func (p *commentParam) pid() string {
	var id string

	id = id + p.ID

	return id
}

type commentMultiGetter struct {
	store *commentCache
	keys  []commentParam
}

type commentQuerySet struct {
	keys    []string
	results map[string]*ent.Comment
}

func (q *commentQuerySet) Get(ID string) (*ent.Comment, error) {
	param := commentParam{

		ID: ID,
	}
	v, ok := q.results[param.pid()]
	if !ok {
		return v, errors.New("not found")
	}
	return v, nil
}

func (q *commentQuerySet) GetSlice() []*ent.Comment {
	var results []*ent.Comment
	for _, k := range q.keys {
		results = append(results, q.results[k])
	}
	return results
}

func (s *commentCache) MGetter() *commentMultiGetter {
	return &commentMultiGetter{
		store: s,
		keys:  []commentParam{},
	}
}

func (g *commentMultiGetter) GetM(ID string) *commentMultiGetter {
	g.keys = append(g.keys, commentParam{ID: ID})
	return g
}

func (g *commentMultiGetter) Do(ctx context.Context) (*commentQuerySet, error) {
	qs := &commentQuerySet{}
	var keys []string
	for _, k := range g.keys {
		pid := k.pid()
		qs.keys = append(qs.keys, pid)
		keys = append(keys, pid)
	}
	if true {
		sort.Strings(keys)
		group := strings.Join(keys, ":")
		data, err, _ := g.store.memo.SingleGroup().Do(group, func() (interface{}, error) {
			return g.pipeDo(ctx)
		})
		qs.results = data.(map[string]*ent.Comment)
		return qs, err
	}
	data, err := g.pipeDo(ctx)
	qs.results = data
	return qs, err
}

func (g *commentMultiGetter) pipeDo(ctx context.Context) (map[string]*ent.Comment, error) {
	pipeline := cacheme.NewPipeline(g.store.client.Redis())
	ps := make(map[string]*CommentPromise)
	for _, k := range g.keys {
		pid := k.pid()
		if _, ok := ps[pid]; ok {
			continue
		}
		promise, err := g.store.GetP(ctx, pipeline, k.ID)
		if err != nil {
			return nil, err
		}
		ps[pid] = promise
	}

	err := pipeline.Execute(ctx)
	if err != nil {
		return nil, err
	}

	results := make(map[string]*ent.Comment)
	for k, p := range ps {
		r, err := p.Result()
		if err != nil {
			return nil, err
		}
		results[k] = r
	}
	return results, nil
}

func (s *commentCache) GetM(ID string) *commentMultiGetter {
	return &commentMultiGetter{
		store: s,
		keys:  []commentParam{{ID: ID}},
	}
}

func (s *commentCache) get(ctx context.Context, ID string) (*ent.Comment, error) {
	param := &commentParam{}

	param.ID = ID

	var t *ent.Comment

	key, err := s.Key(param)
	if err != nil {
		return t, err
	}

	memo := s.memo
	var res []byte

	res, err = memo.GetCached(ctx, key)
	if err == nil {
		s.client.logger.Log(s.tag, key, Hit)
		err = cacheme.Unmarshal(res, &t)
		return t, err
	}

	if err != redis.Nil {
		return t, errors.New("")
	}
	s.client.logger.Log(s.tag, key, Miss)

	resourceLock, err := memo.Lock(ctx, key)
	if err != nil {
		return t, err
	}

	if resourceLock {
		s.client.logger.Log(s.tag, key, Fetch)
		value, err := s.Fetch(ctx, ID)
		if err != nil {
			return value, err
		}
		packed, err := cacheme.Marshal(value)
		if err == nil {
			memo.SetCache(ctx, key, packed, time.Millisecond*360000000)
			memo.AddGroup(ctx, s.Group(), key)
		}
		return value, err
	}

	res, err = memo.Wait(ctx, key)

	if err == nil {
		err = cacheme.Unmarshal(res, &t)
		return t, err
	}
	return t, err
}

func (s *commentCache) Update(ctx context.Context, ID string) error {

	param := &commentParam{}

	param.ID = ID

	key, err := s.Key(param)
	if err != nil {
		return err
	}

	value, err := s.Fetch(ctx, ID)
	if err != nil {
		return err
	}
	packed, err := cacheme.Marshal(value)
	if err == nil {
		s.memo.SetCache(ctx, key, packed, time.Millisecond*360000000)
		s.memo.AddGroup(ctx, s.Group(), key)
	}
	return err
}

func (s *commentCache) Invalid(ctx context.Context, ID string) error {

	param := &commentParam{}

	param.ID = ID

	key, err := s.Key(param)
	if err != nil {
		return err
	}
	return s.memo.DeleteCache(ctx, key)

}

func (s *commentCache) InvalidAll(ctx context.Context, version string) error {
	group := s.versionedGroup(version)
	if s.client.cluster {
		return cacheme.InvalidAllCluster(ctx, group, s.client.redis)
	}
	return cacheme.InvalidAll(ctx, group, s.client.redis)

}

type commentListIDSCache struct {
	Fetch       func(ctx context.Context) ([]int, error)
	tag         string
	memo        *cacheme.RedisMemoLock
	client      *Client
	version     string
	versionFunc func() string
}

type CommentListIDSPromise struct {
	executed     chan bool
	redisPromise *redis.StringCmd
	result       []int
	error        error
	store        *commentListIDSCache
	ctx          context.Context
}

func (p *CommentListIDSPromise) WaitExecute(cp *cacheme.CachePipeline, key string) {
	defer cp.Wg.Done()
	var t []int
	memo := p.store.memo

	<-cp.Executed
	value, err := p.redisPromise.Bytes()
	if err == nil {
		p.store.client.logger.Log(p.store.tag, key, Hit)
		err = cacheme.Unmarshal(value, &t)
		p.result, p.error = t, err
		return
	}

	resourceLock, err := memo.Lock(p.ctx, key)
	if err != nil {
		p.error = err
		return
	}
	p.store.client.logger.Log(p.store.tag, key, Miss)

	if resourceLock {
		p.store.client.logger.Log(p.store.tag, key, Fetch)
		value, err := p.store.Fetch(
			p.ctx,
		)
		if err != nil {
			p.error = err
			return
		}
		p.result = value
		packed, err := cacheme.Marshal(value)
		if err == nil {
			memo.SetCache(p.ctx, key, packed, time.Millisecond*360000000)
			memo.AddGroup(p.ctx, p.store.Group(), key)
		}
		p.error = err
		return
	}

	var res []byte
	if true {
		res, err = memo.WaitSingle(p.ctx, key)
	} else {
		res, err = memo.Wait(p.ctx, key)
	}
	if err == nil {
		err = cacheme.Unmarshal(res, &t)
	}
	p.result, p.error = t, err
}

func (p *CommentListIDSPromise) Result() ([]int, error) {
	return p.result, p.error
}

var CommentListIDSCacheStore = &commentListIDSCache{tag: "CommentListIDS"}

func (s *commentListIDSCache) SetClient(c *Client) {
	s.client = c
}

func (s *commentListIDSCache) Clone(r cacheme.RedisClient) *commentListIDSCache {
	value := *s
	new := &value
	lock, err := cacheme.NewRedisMemoLock(
		context.TODO(), "cacheme", r, s.tag, 5*time.Second,
	)
	if err != nil {
		fmt.Println(err)
	}
	new.memo = lock

	return new
}

func (s *commentListIDSCache) Version() string {
	if s.versionFunc != nil {
		return s.versionFunc()
	}
	return s.version
}

func (s *commentListIDSCache) KeyTemplate() string {
	return "commentids" + ":v" + s.Version()
}

func (s *commentListIDSCache) Key(p *commentListIDSParam) (string, error) {
	t := template.Must(template.New("").Parse(s.KeyTemplate()))
	t = t.Option("missingkey=zero")
	var tpl bytes.Buffer
	err := t.Execute(&tpl, p)
	return tpl.String(), err
}

func (s *commentListIDSCache) Group() string {
	return "cacheme" + ":group:" + s.tag + ":v" + s.Version()
}

func (s *commentListIDSCache) versionedGroup(v string) string {
	return "cacheme" + ":group:" + s.tag + ":v" + v
}

func (s *commentListIDSCache) AddMemoLock() error {
	lock, err := cacheme.NewRedisMemoLock(context.TODO(), "cacheme", s.client.redis, s.tag, 5*time.Second)
	if err != nil {
		return err
	}

	s.memo = lock
	return nil
}

func (s *commentListIDSCache) Initialized() bool {
	return s.Fetch != nil
}

func (s *commentListIDSCache) Tag() string {
	return s.tag
}

func (s *commentListIDSCache) GetP(ctx context.Context, pp *cacheme.CachePipeline) (*CommentListIDSPromise, error) {
	param := &commentListIDSParam{}

	key, err := s.Key(param)
	if err != nil {
		return nil, err
	}

	cacheme := s.memo

	promise := &CommentListIDSPromise{
		executed: pp.Executed,
		ctx:      ctx,
		store:    s,
	}

	wait := cacheme.GetCachedP(ctx, pp.Pipeline, key)
	promise.redisPromise = wait
	pp.Wg.Add(1)
	go promise.WaitExecute(
		pp, key)
	return promise, nil
}

func (s *commentListIDSCache) Get(ctx context.Context) ([]int, error) {

	param := &commentListIDSParam{}

	var t []int

	key, err := s.Key(param)
	if err != nil {
		return t, err
	}

	if true {
		data, err, _ := s.memo.SingleGroup().Do(key, func() (interface{}, error) {
			return s.get(ctx)
		})
		return data.([]int), err
	}
	return s.get(ctx)
}

type commentListIDSParam struct {
}

func (p *commentListIDSParam) pid() string {
	var id string

	return id
}

type commentListIDSMultiGetter struct {
	store *commentListIDSCache
	keys  []commentListIDSParam
}

type commentListIDSQuerySet struct {
	keys    []string
	results map[string][]int
}

func (q *commentListIDSQuerySet) Get() ([]int, error) {
	param := commentListIDSParam{}
	v, ok := q.results[param.pid()]
	if !ok {
		return v, errors.New("not found")
	}
	return v, nil
}

func (q *commentListIDSQuerySet) GetSlice() [][]int {
	var results [][]int
	for _, k := range q.keys {
		results = append(results, q.results[k])
	}
	return results
}

func (s *commentListIDSCache) MGetter() *commentListIDSMultiGetter {
	return &commentListIDSMultiGetter{
		store: s,
		keys:  []commentListIDSParam{},
	}
}

func (g *commentListIDSMultiGetter) GetM() *commentListIDSMultiGetter {
	g.keys = append(g.keys, commentListIDSParam{})
	return g
}

func (g *commentListIDSMultiGetter) Do(ctx context.Context) (*commentListIDSQuerySet, error) {
	qs := &commentListIDSQuerySet{}
	var keys []string
	for _, k := range g.keys {
		pid := k.pid()
		qs.keys = append(qs.keys, pid)
		keys = append(keys, pid)
	}
	if true {
		sort.Strings(keys)
		group := strings.Join(keys, ":")
		data, err, _ := g.store.memo.SingleGroup().Do(group, func() (interface{}, error) {
			return g.pipeDo(ctx)
		})
		qs.results = data.(map[string][]int)
		return qs, err
	}
	data, err := g.pipeDo(ctx)
	qs.results = data
	return qs, err
}

func (g *commentListIDSMultiGetter) pipeDo(ctx context.Context) (map[string][]int, error) {
	pipeline := cacheme.NewPipeline(g.store.client.Redis())
	ps := make(map[string]*CommentListIDSPromise)
	for _, k := range g.keys {
		pid := k.pid()
		if _, ok := ps[pid]; ok {
			continue
		}
		promise, err := g.store.GetP(ctx, pipeline)
		if err != nil {
			return nil, err
		}
		ps[pid] = promise
	}

	err := pipeline.Execute(ctx)
	if err != nil {
		return nil, err
	}

	results := make(map[string][]int)
	for k, p := range ps {
		r, err := p.Result()
		if err != nil {
			return nil, err
		}
		results[k] = r
	}
	return results, nil
}

func (s *commentListIDSCache) GetM() *commentListIDSMultiGetter {
	return &commentListIDSMultiGetter{
		store: s,
		keys:  []commentListIDSParam{{}},
	}
}

func (s *commentListIDSCache) get(ctx context.Context) ([]int, error) {
	param := &commentListIDSParam{}

	var t []int

	key, err := s.Key(param)
	if err != nil {
		return t, err
	}

	memo := s.memo
	var res []byte

	res, err = memo.GetCached(ctx, key)
	if err == nil {
		s.client.logger.Log(s.tag, key, Hit)
		err = cacheme.Unmarshal(res, &t)
		return t, err
	}

	if err != redis.Nil {
		return t, errors.New("")
	}
	s.client.logger.Log(s.tag, key, Miss)

	resourceLock, err := memo.Lock(ctx, key)
	if err != nil {
		return t, err
	}

	if resourceLock {
		s.client.logger.Log(s.tag, key, Fetch)
		value, err := s.Fetch(ctx)
		if err != nil {
			return value, err
		}
		packed, err := cacheme.Marshal(value)
		if err == nil {
			memo.SetCache(ctx, key, packed, time.Millisecond*360000000)
			memo.AddGroup(ctx, s.Group(), key)
		}
		return value, err
	}

	res, err = memo.Wait(ctx, key)

	if err == nil {
		err = cacheme.Unmarshal(res, &t)
		return t, err
	}
	return t, err
}

func (s *commentListIDSCache) Update(ctx context.Context) error {

	param := &commentListIDSParam{}

	key, err := s.Key(param)
	if err != nil {
		return err
	}

	value, err := s.Fetch(ctx)
	if err != nil {
		return err
	}
	packed, err := cacheme.Marshal(value)
	if err == nil {
		s.memo.SetCache(ctx, key, packed, time.Millisecond*360000000)
		s.memo.AddGroup(ctx, s.Group(), key)
	}
	return err
}

func (s *commentListIDSCache) Invalid(ctx context.Context) error {

	param := &commentListIDSParam{}

	key, err := s.Key(param)
	if err != nil {
		return err
	}
	return s.memo.DeleteCache(ctx, key)

}

func (s *commentListIDSCache) InvalidAll(ctx context.Context, version string) error {
	group := s.versionedGroup(version)
	if s.client.cluster {
		return cacheme.InvalidAllCluster(ctx, group, s.client.redis)
	}
	return cacheme.InvalidAll(ctx, group, s.client.redis)

}

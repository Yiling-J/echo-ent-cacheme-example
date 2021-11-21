# echo-ent-cacheme-example

Example application of Echo (Go web framework) and Entgo (ORM) and Cacheme (Caching framework).

## Usage

Update `config/dev.toml` `database.pg.dsn` to PostgreSQL connection string.

Update `config/dev.toml` `database.redis.address` to Redis connection string(local example: localhost:6379).

## Installation

```
$ git clone https://github.com/Yiling-J/echo-ent-cacheme-example
$ cd echo-ent-cacheme-example
$ go build
```

## Benchmark

wrk pipeline script:
```lua
init = function(args)
   local r = {}
   r[1] = wrk.format(nil, "/api/comments")
   r[2] = wrk.format(nil, "/api/comments/1")
   r[3] = wrk.format(nil, "/api/comments/2")
   r[4] = wrk.format(nil, "/api/comments/3")

   req = table.concat(r)
end

request = function()
   return req
end
```

wrk command:
```
wrk -t10 -c200 -d60s http://127.0.0.1:8989/ -s pipeline.lua
```

#### Benchmark Results:

- Read only:
```
Running 1m test @ http://127.0.0.1:8989/
  10 threads and 200 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency    29.42ms   96.29ms   1.03s    97.03%
    Req/Sec     4.04k   381.68     4.97k    87.49%
  2358796 requests in 1.00m, 1.44GB read
  Socket errors: connect 0, read 62, write 0, timeout 0
Requests/sec:  39278.88
Transfer/sec:     24.63MB
```
- Insert a comment every 1 second:

Change `config/base.toml` `comment.auto_insert` to true, build again and start server
```
Running 1m test @ http://127.0.0.1:8989/
  10 threads and 200 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency    34.06ms  113.13ms   1.03s    94.30%
    Req/Sec     3.91k   428.53     4.85k    81.55%
  2268729 requests in 1.00m, 1.39GB read
  Socket errors: connect 0, read 64, write 0, timeout 0
Requests/sec:  37765.96
Transfer/sec:     23.68MB
```

## License

MIT

## Author

Yasuhiro Matsumoto (a.k.a. mattn)

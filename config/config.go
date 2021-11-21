//nolint
package config

type comment struct {
	Autoinsert string
}

var Comment = comment{

	Autoinsert: "comment.auto_insert",
}

type database struct {
	Pg databasePg

	Redis databaseRedis
}

var Database = database{

	Pg: DatabasePg,

	Redis: DatabaseRedis,
}

type databasePg struct {
	Dsn string

	Maxidleconnections string

	Maxopenconnections string
}

var DatabasePg = databasePg{

	Dsn: "database.pg.dsn",

	Maxidleconnections: "database.pg.max_idle_connections",

	Maxopenconnections: "database.pg.max_open_connections",
}

type databaseRedis struct {
	Address string

	Db string
}

var DatabaseRedis = databaseRedis{

	Address: "database.redis.address",

	Db: "database.redis.db",
}

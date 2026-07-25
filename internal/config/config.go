package config

import "fmt"

type Pool struct {
	min int32
	max int32
}

type Database struct {
	Pool
	host     string
	username string
	password string
	database string
}

func (d Database) connStr() string {
	return "host=" + d.host + " user=" + d.username + " password=" + d.password + " dbname=" + d.database
}

func (d Database) DSN() string {
	return fmt.Sprintf("pgx5://%s:%s@%s:5432/%s", d.username, d.password, d.host, d.database)
}

func DefaultDatabase() Database {
	return Database{
		host:     "localhost",
		username: "postgres",
		password: "postgres",
		database: "demo",
		Pool: Pool{
			min: 1,
			max: 3,
		},
	}
}

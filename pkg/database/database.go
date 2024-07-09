package database

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

type Database struct {
	*sqlx.DB
}

func NewDatabase(host, port, user, password, dbname string) (*Database, error) {
	authString := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=disable", host, port, user, dbname)
	if password != "" {
		authString += " password=" + password
	}

	db, err := sqlx.Connect("postgres", authString)
	if err != nil {
		return nil, err
	}

	return &Database{db}, nil
}

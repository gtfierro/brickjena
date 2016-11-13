package main

import (
	"github.com/boltdb/bolt"
	"log"
)

type SavedQueries struct {
	db *bolt.DB
}

func NewSavedQueryDB(path string) *SavedQueries {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
	sq := &SavedQueries{
		db: db,
	}

	return sq
}

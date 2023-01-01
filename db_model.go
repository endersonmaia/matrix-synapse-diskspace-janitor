package main

import (
	"database/sql"
	"log"

	errors "git.sequentialread.com/forest/pkg-errors"
	_ "github.com/lib/pq"
)

type DBModel struct {
	DB *sql.DB
}

type StateGroupsStateStream struct {
	EstimatedCount int
	Channel        chan StateGroupsStateRow
}

type StateGroupsStateRow struct {
	StateGroup int64
	Type       string
	StateKey   string
	RoomID     string
	EventId    string
}

func initDatabase(config *Config) *DBModel {

	db, err := sql.Open(config.DatabaseType, config.DatabaseConnectionString)
	if err != nil {
		log.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("failed to open database connection: %+v", err)
	}

	return &DBModel{
		DB: db,
	}
}

func (model *DBModel) StateGroupsStateStream() (*StateGroupsStateStream, error) {
	var estimatedCount int
	err := model.DB.QueryRow(`
		SELECT reltuples::bigint FROM pg_class WHERE oid = 'public.state_groups_state'::regclass;
	`).Scan(&estimatedCount)

	if err != nil {
		return nil, errors.Wrap(err, "could not get estimated row count of state_groups_state")
	}

	rows, err := model.DB.Query("SELECT state_group, type, state_key, room_id FROM state_groups_state")
	if err != nil {
		return nil, errors.Wrap(err, "could not select from state_groups_state")
	}
	toReturn := StateGroupsStateStream{
		EstimatedCount: estimatedCount,
		Channel:        make(chan StateGroupsStateRow, 10000),
	}

	go func(rows *sql.Rows, channel chan StateGroupsStateRow) {
		defer rows.Close()

		for rows.Next() {
			var stateGroup int64
			var tyype string
			var stateKey string
			var roomID string
			err := rows.Scan(&stateGroup, &tyype, &stateKey, &roomID)
			if err != nil {
				log.Printf("error scanning a state_groups_state row: %s \n", err)
			} else {
				channel <- StateGroupsStateRow{
					StateGroup: stateGroup,
					Type:       tyype,
					StateKey:   stateKey,
					RoomID:     roomID,
				}
			}
		}

		close(channel)

	}(rows, toReturn.Channel)

	return &toReturn, nil
}

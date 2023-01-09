package main

import (
	"database/sql"
	"fmt"
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

type DeleteStateGroupsStateStatus struct {
	StateGroupsDeleted int64
	RowsDeleted        int64
	Errors             int64
}

type DBTableSize struct {
	Schema string
	Name   string
	Bytes  int64
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
		Channel:        make(chan StateGroupsStateRow, 50000),
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

func (model *DBModel) GetStateGroupsForRoom(roomId string) (stateGroupIds []int64, err error) {

	rows, err := model.DB.Query("SELECT id from state_groups where room_id = $1", roomId)
	if err != nil {
		return nil, errors.Wrap(err, "could not select from state_groups by room_id")
	}

	stateGroupIds = []int64{}
	for rows.Next() {
		var stateGroupId int64

		err := rows.Scan(&stateGroupId)
		if err != nil {
			log.Printf("error scanning a state_group id: %s \n", err)
		} else {
			stateGroupIds = append(stateGroupIds, stateGroupId)
		}
	}

	return stateGroupIds, nil
}

func (model *DBModel) DeleteStateGroupsForRoom(roomId string) (int64, error) {

	rowsDeleted := int64(0)

	// state_group_edges
	result, err := model.DB.Exec(
		"DELETE FROM state_group_edges where state_group in (SELECT id from state_groups where room_id = $1);", roomId,
	)
	if err != nil {
		return -1, errors.Wrap(err, "could not delete state_group_edges by room_id")
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return -1, errors.Wrap(err, "could not get # of rows affected for delete state_group_edges by room_id")
	}
	rowsDeleted += int64(affected)

	// event_to_state_groups
	result, err = model.DB.Exec(
		"DELETE FROM event_to_state_groups where state_group in (SELECT id from state_groups where room_id = $1);", roomId,
	)
	if err != nil {
		return -1, errors.Wrap(err, "could not delete event_to_state_groups by room_id")
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return -1, errors.Wrap(err, "could not get # of rows affected for delete event_to_state_groups by room_id")
	}
	rowsDeleted += int64(affected)

	// state_groups
	result, err = model.DB.Exec(
		"DELETE FROM state_groups where room_id = $1;", roomId,
	)
	if err != nil {
		return -1, errors.Wrap(err, "could not delete state_groups by room_id")
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return -1, errors.Wrap(err, "could not get # of rows affected for delete state_groups by room_id")
	}
	rowsDeleted += int64(affected)

	return rowsDeleted, nil
}

// TODO this maybe should be parallelized to reduce back-and-forth?  (latency-to-db issues)
// But operating on a sorted list of stateGroup IDs seems to work pretty well
// I'll leave it as-is for now
func (model *DBModel) DeleteStateGroupsState(stateGroupIds []int64, startAt int) chan DeleteStateGroupsStateStatus {

	toReturn := make(chan DeleteStateGroupsStateStatus, 100)

	go func(stateGroupIds []int64, startAt int, channel chan DeleteStateGroupsStateStatus) {
		var rowsDeleted int64 = 0
		var errorCount int64 = 0
		for i := startAt; i < len(stateGroupIds); i++ {
			result, err := model.DB.Exec(
				"DELETE FROM state_groups_state where state_group = $1;", stateGroupIds[i],
			)
			if err != nil {
				fmt.Println(errors.Wrap(err, "could not delete from state_groups_state by state_group"))
				errorCount += 1
				continue
			}
			affected, err := result.RowsAffected()
			if err != nil {
				fmt.Println(errors.Wrap(err, "could not get # of rows affected for delete from state_groups_state by state_group"))
				errorCount += 1
				continue
			}
			rowsDeleted += affected
			channel <- DeleteStateGroupsStateStatus{
				StateGroupsDeleted: int64(i) + 1,
				RowsDeleted:        rowsDeleted,
				Errors:             errorCount,
			}
		}
	}(stateGroupIds, startAt, toReturn)

	return toReturn
}

// https://dataedo.com/kb/query/postgresql/list-of-tables-by-their-size
func (model *DBModel) GetDBTableSizes() (tables []DBTableSize, err error) {

	rows, err := model.DB.Query(
		`select schemaname as table_schema, relname as table_name, pg_relation_size(relid) as data_size 
		from pg_catalog.pg_statio_user_tables
		`,
	)
	if err != nil {
		return nil, errors.Wrap(err, "could not get table sizes as bytes")
	}

	tables = []DBTableSize{}
	for rows.Next() {
		var schema string
		var name string
		var bytez int64

		err := rows.Scan(&schema, &name, &bytez)
		if err != nil {
			log.Printf("error scanning a table size row: %s \n", err)
		} else {
			tables = append(tables, DBTableSize{
				Schema: schema,
				Name:   name,
				Bytes:  bytez,
			})
		}
	}

	return tables, nil
}

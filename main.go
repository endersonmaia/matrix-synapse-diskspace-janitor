package main

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	configlite "git.sequentialread.com/forest/config-lite"
)

type Config struct {
	FrontendPort             int
	FrontendDomain           string
	MatrixURL                string
	MatrixServerPublicDomain string
	AdminMatrixRoomId        string
	MatrixAdminToken         string
	DatabaseType             string
	DatabaseConnectionString string
	MediaFolder              string
	PostgresFolder           string
}

type JanitorState struct {
	LastScheduledTaskRunUnixMilli int64
}

type DiskUsage struct {
	DiskSizeBytes int64
	OtherBytes    int64
	MediaBytes    int64
	PostgresBytes int64
}

var isRunningScheduledTask bool
var isDoingDeletes bool
var mutex sync.Mutex
var matrixAdmin *MatrixAdmin

func main() {
	mutex = sync.Mutex{}

	config := Config{}
	ignoreCommandlineFlags := []string{}
	err := configlite.ReadConfiguration("config.json", "JANITOR", ignoreCommandlineFlags, reflect.ValueOf(&config))
	if err != nil {
		panic(err)
	}

	validateConfig(&config)

	os.MkdirAll("data", 0644)
	os.MkdirAll("data/sessions", 0644)

	db := initDatabase(&config)
	matrixAdmin = initMatrixAdmin(&config)
	frontend := initFrontend(&config, db)

	log.Printf("🧹 matrix-synapse-diskspace-janitor is about to try to start listening on :%d\n", config.FrontendPort)
	go frontend.ListenAndServe()

	// resume a previously stopped delete
	deleteRooms, err := ReadJsonFile[DeleteProgress]("data/deleteRooms.json")
	if err != nil {
		log.Printf("ERROR!: can't read data/deleteRooms.json: %+v\n", err)
	} else if deleteRooms.Rooms != nil && len(deleteRooms.Rooms) != 0 {
		go doRoomDeletes(db)
	}

	for {
		janitorState, err := ReadJsonFile[JanitorState]("data/janitorState.json")
		if err != nil {
			log.Printf("ERROR!: can't read data/janitorState.json: %+v\n", err)
		} else {
			sinceLastScheduledTaskDuration := time.Since(time.UnixMilli(janitorState.LastScheduledTaskRunUnixMilli))
			if !isRunningScheduledTask && sinceLastScheduledTaskDuration > time.Hour*24 {
				go runScheduledTask(db, &config, true, true)

			}
		}

		time.Sleep(time.Second * 10)
	}
}

func runScheduledTask(db *DBModel, config *Config, measureMediaSize bool, stateGroupsStateScan bool) {

	isRunningScheduledTask = true
	log.Println("starting runScheduledTask...")

	originalDiskUsage, err := ReadJsonFile[DiskUsage]("data/diskUsage.json")
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't read data/diskUsage.json: %s\n", err)
	}

	log.Println("GetDBTableSizes...")
	tables, err := db.GetDBTableSizes()
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't GetDBTableSizes: %s\n", err)
	}
	log.Println("Saving data/dbTableSizes.json...")
	err = WriteJsonFile[[]DBTableSize]("data/dbTableSizes.json", tables)
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't write data/dbTableSizes.json: %s\n", err)
	}

	log.Println("GetAvaliableDiskSpace...")
	availableBytes, totalBytes, err := GetAvaliableDiskSpace(config.MediaFolder)
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't GetAvaliableDiskSpace: %s\n", err)
	}

	var mediaBytes int64
	if measureMediaSize {
		log.Printf("GetTotalFilesizeWithinFolder(\"%s\")...\n", config.MediaFolder)
		mediaBytes, err = GetTotalFilesizeWithinFolder(config.MediaFolder)
		if err != nil {
			log.Printf("ERROR!: runScheduledTask can't GetTotalFilesizeWithinFolder(\"%s\"): %s\n", config.MediaFolder, err)
		}
	} else {
		mediaBytes = originalDiskUsage.MediaBytes
	}

	log.Printf("GetTotalFilesizeWithinFolder(\"%s\")...\n", config.PostgresFolder)
	postgresBytes, err := GetTotalFilesizeWithinFolder(config.PostgresFolder)
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't GetTotalFilesizeWithinFolder(\"%s\"): %s\n", config.PostgresFolder, err)
	}

	diskUsage := DiskUsage{
		DiskSizeBytes: totalBytes,
		OtherBytes:    (totalBytes - availableBytes) - (mediaBytes + postgresBytes),
		MediaBytes:    mediaBytes,
		PostgresBytes: postgresBytes,
	}

	const 
	const diskUsagePercent = 

	log.Println("Saving data/diskUsage.json...")
	err = WriteJsonFile("data/diskUsage.json", diskUsage)
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't write data/diskUsage.json: %s\n", err)
	}

	if stateGroupsStateScan {
		log.Println("starting db.StateGroupsStateStream()...")
		stream, err := db.StateGroupsStateStream()
		if err != nil {
			log.Fatalf("Can't start because %+v\n", err)
		}

		lastUpdateTime := time.Now()
		updateCounter := 0
		rowCounter := 0
		rowCountByRoom := map[string]int{}

		for row := range stream.Channel {
			rowCountByRoom[row.RoomID] = rowCountByRoom[row.RoomID] + 1
			updateCounter += 1
			rowCounter += 1
			if updateCounter > 10000 {
				if time.Now().After(lastUpdateTime.Add(time.Second * 60)) {
					lastUpdateTime = time.Now()
					percent := int((float64(rowCounter) / float64(stream.EstimatedCount)) * float64(100))
					log.Printf("state_groups_state table scan %d/%d (%d%s) ... \n", rowCounter, stream.EstimatedCount, percent, "%")
				}
				updateCounter = 0
			}
		}

		err = WriteJsonFile("data/stateGroupsStateRowCountByRoom.json", rowCountByRoom)
		if err != nil {
			log.Printf("ERROR!: runScheduledTask can't write data/stateGroupsStateRowCountByRoom.json: %s\n", err)
		}
	}

	log.Println("updating data/janitorState.json...")

	janitorState, err := ReadJsonFile[JanitorState]("data/janitorState.json")
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't read data/janitorState.json: %+v\n", err)
	}

	janitorState.LastScheduledTaskRunUnixMilli = time.Now().UnixMilli()

	err = WriteJsonFile("data/janitorState.json", janitorState)
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't write data/janitorState.json: %s\n", err)
	}


	
	log.Println("runScheduledTask completed!")
	isRunningScheduledTask = false
}

func doRoomDeletes(db *DBModel) {
	if isDoingDeletes {
		log.Println("doRoomDeletes(): isDoingDeletes already!")
		return
	}
	isDoingDeletes = true
	defer func() {
		isDoingDeletes = false
	}()

	deleteProgress, err := ReadJsonFile[DeleteProgress]("data/deleteRooms.json")
	if err != nil {
		log.Println("doRoomDeletes(): Can't do room deletes because can't read deleteRooms.json")
		return
	}

	if deleteProgress.Rooms == nil || len(deleteProgress.Rooms) == 0 {
		log.Println("doRoomDeletes(): Can't do room deletes because no rooms to delete")
		return
	}

	log.Printf("doRoomDeletes(): starting to delete %d rooms\n", len(deleteProgress.Rooms))

	for _, room := range deleteProgress.Rooms {
		err := matrixAdmin.DeleteRoom(room.Id, room.Ban)
		if err != nil {
			log.Printf("doRoomDeletes(): Can't do room deletes because deleting %s returned %s\n", room.Id, err)
			return
		}
	}

	log.Printf("doRoomDeletes(): waiting for %d rooms to be done deleting...\n", len(deleteProgress.Rooms))

	isDoneWaitingForRoomDeletesToFinish := false
	for !isDoneWaitingForRoomDeletesToFinish {

		allRoomsDeletionComplete := true
		for i, room := range deleteProgress.Rooms {
			// TODO do something with the users that this returns? i.e. re-add them to the room later?
			status, _, err := matrixAdmin.GetDeleteRoomStatus(room.Id)
			if err != nil {
				log.Printf("doRoomDeletes(): Can't do room deletes because GetDeleteRoomStatus('%s') returned %s\n", room.Id, err)
				return
			}
			deleteProgress.Rooms[i] = MatrixRoom{
				Id:         room.Id,
				IdWithName: room.IdWithName,
				Ban:        room.Ban,
				Status:     status,
			}

			if status != "complete" {
				allRoomsDeletionComplete = false
			}
		}

		err = WriteJsonFile("data/deleteRooms.json", deleteProgress)
		if err != nil {
			log.Println("doRoomDeletes(): Can't do room deletes because can't write deleteRooms.json")
			return
		}

		if allRoomsDeletionComplete {
			isDoneWaitingForRoomDeletesToFinish = true
		} else {
			time.Sleep(time.Second * 5)
		}
	}

	log.Printf("doRoomDeletes(): getting state group ids for %d rooms...\n", len(deleteProgress.Rooms))

	allStateGroupsToDelete := []int64{}
	for _, room := range deleteProgress.Rooms {
		log.Printf("db.GetStateGroupsForRoom(%s)\n", room.Id)
		stateGroups, err := db.GetStateGroupsForRoom(room.Id)
		if err != nil {
			log.Printf("doRoomDeletes(): Can't do room deletes because getting state group ids for %s returned %s\n", room.Id, err)
			return
		}
		allStateGroupsToDelete = append(allStateGroupsToDelete, stateGroups...)
	}

	sort.Slice(allStateGroupsToDelete, func(i, j int) bool {
		return allStateGroupsToDelete[i] < allStateGroupsToDelete[j]
	})

	allStateGroupsToDeleteFile, err := os.OpenFile("data/stateGroupsToDelete.txt", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	for _, stateGroupId := range allStateGroupsToDelete {
		fmt.Fprintf(allStateGroupsToDeleteFile, "%d\n", stateGroupId)
	}
	allStateGroupsToDeleteFile.Close()

	log.Printf("doRoomDeletes(): deleting %d state groups from  state_groups_state...\n", len(allStateGroupsToDelete))

	statusChannel := db.DeleteStateGroupsState(allStateGroupsToDelete, 0)
	lastUpdateTime := time.Now()
	for status := range statusChannel {
		if time.Since(lastUpdateTime) > time.Second*5 {
			lastUpdateTime = time.Now()
			deleteProgress.StateGroupsStateProgress = int((float64(status.StateGroupsDeleted) / float64(len(allStateGroupsToDelete))) * float64(100))

			log.Printf(
				"doRoomDeletes(): %d/%d (%d rows) (%d errors) (%d%s)\n",
				status.StateGroupsDeleted, len(allStateGroupsToDelete), status.RowsDeleted,
				status.Errors, deleteProgress.StateGroupsStateProgress, "%",
			)

			err = WriteJsonFile("data/deleteRooms.json", deleteProgress)
			if err != nil {
				log.Println("doRoomDeletes(): Can't do room deletes because can't write deleteRooms.json")
				return
			}
		}
	}

	log.Println("doRoomDeletes(): deleting from state_groups_state complete! now cleaning up state_groups...")

	totalStateGroupRows := 0
	for _, room := range deleteProgress.Rooms {
		rowsDeleted, err := db.DeleteStateGroupsForRoom(room.Id)
		if err != nil {
			log.Printf("doRoomDeletes(): DeleteStateGroupsForRoom('%s') returned %s\n", room.Id, err)
		}
		totalStateGroupRows += int(rowsDeleted)
	}

	log.Printf("doRoomDeletes(): %d state_groups related rows deleted. \n", totalStateGroupRows)

	err = os.Remove("data/deleteRooms.json")
	if err != nil {
		log.Printf("doRoomDeletes(): failed to remove deleteRooms.json: %s\n", err)
	}

	log.Println("doRoomDeletes(): completed successfully!!")
}

func validateConfig(config *Config) {

	errors := []string{}

	if config.FrontendPort == 0 {
		errors = append(errors, "Can't start because FrontendPort is required")
	}
	if config.FrontendDomain == "" {
		errors = append(errors, "Can't start because FrontendDomain is required")
	}
	if config.MatrixURL == "" {
		errors = append(errors, "Can't start because MatrixURL is required")
	}
	if config.MatrixAdminToken == "" || config.MatrixAdminToken == "changeme" {
		errors = append(errors, "Can't start because MatrixAdminToken is required")
	}
	if config.MatrixServerPublicDomain == "" {
		errors = append(errors, "Can't start because MatrixServerPublicDomain is required")
	}
	if config.AdminMatrixRoomId == "" {
		errors = append(errors, "Can't start because AdminMatrixRoomId is required")
	}
	if config.DatabaseType == "" {
		errors = append(errors, "Can't start because DatabaseType is required")
	}
	if config.DatabaseConnectionString == "" {
		errors = append(errors, "Can't start because DatabaseConnectionString is required")
	}
	if config.MediaFolder == "" {
		errors = append(errors, "Can't start because MediaFolder is required")
	}
	if config.PostgresFolder == "" {
		errors = append(errors, "Can't start because PostgresFolder is required")
	}

	if len(errors) > 0 {
		log.Fatalln(strings.Join(errors, "\n"))
	}
}

package main

import (
	"log"
	"os"
	"reflect"
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

	//db := initDatabase(&config)
	matrixAdmin = initMatrixAdmin(&config)
	frontend := initFrontend(&config)

	log.Printf("🧹 matrix-synapse-diskspace-janitor is about to try to start listening on :%d\n", config.FrontendPort)
	go frontend.ListenAndServe()

	for {
		janitorState, err := ReadJsonFile[JanitorState]("data/janitorState.json")
		if err != nil {
			log.Printf("ERROR!: can't read data/janitorState.json: %+v\n", err)
		} else {
			sinceLastScheduledTaskDuration := time.Since(time.UnixMilli(janitorState.LastScheduledTaskRunUnixMilli))
			if !isRunningScheduledTask && sinceLastScheduledTaskDuration > time.Hour*24 {
				//go runScheduledTask(db, &config)

			}
		}

		time.Sleep(time.Second * 10)
	}
}

func runScheduledTask(db *DBModel, config *Config) {

	isRunningScheduledTask = true
	log.Println("starting runScheduledTask...")

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

	log.Printf("GetTotalFilesizeWithinFolder(\"%s\")...\n", config.MediaFolder)
	mediaBytes, err := GetTotalFilesizeWithinFolder(config.MediaFolder)
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't GetTotalFilesizeWithinFolder(\"%s\"): %s\n", config.MediaFolder, err)
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

	log.Println("Saving data/diskUsage.json...")
	err = WriteJsonFile[DiskUsage]("data/diskUsage.json", diskUsage)
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't write data/diskUsage.json: %s\n", err)
	}

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

	err = WriteJsonFile[map[string]int]("data/stateGroupsStateRowCountByRoom.json", rowCountByRoom)
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't write data/stateGroupsStateRowCountByRoom.json: %s\n", err)
	}

	log.Println("updating data/janitorState.json...")

	janitorState, err := ReadJsonFile[JanitorState]("data/janitorState.json")
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't read data/janitorState.json: %+v\n", err)
	}

	janitorState.LastScheduledTaskRunUnixMilli = time.Now().UnixMilli()

	err = WriteJsonFile[JanitorState]("data/janitorState.json", janitorState)
	if err != nil {
		log.Printf("ERROR!: runScheduledTask can't write data/janitorState.json: %s\n", err)
	}

	log.Println("runScheduledTask completed!")
	isRunningScheduledTask = false
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

package main

import (
	"encoding/json"
	"log"
	"os"
	"reflect"
	"time"

	configlite "git.sequentialread.com/forest/config-lite"
)

type Config struct {
	MatrixURL                string
	MatrixAdminToken         string
	DatabaseType             string
	DatabaseConnectionString string
}

func main() {
	config := Config{}
	ignoreCommandlineFlags := []string{}
	err := configlite.ReadConfiguration("config.json", "JANITOR", ignoreCommandlineFlags, reflect.ValueOf(&config))
	if err != nil {
		panic(err)
	}

	db := initDatabase(&config)
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
			if time.Now().After(lastUpdateTime.Add(time.Second)) {
				lastUpdateTime = time.Now()
				percent := int((float64(rowCounter) / float64(stream.EstimatedCount)) * float64(100))
				log.Printf("%d/%d (%d%s) ... \n", rowCounter, stream.EstimatedCount, percent, "%")
			}
			updateCounter = 0
		}
	}

	output, err := json.MarshalIndent(rowCountByRoom, "", "  ")

	if err != nil {
		log.Printf("Can't save rooms.json because json.MarshalIndent returned %+v\n", err)
	}

	err = os.WriteFile("./rooms.json", output, 0755)

	if err != nil {
		log.Printf("Can't save rooms.json because os.WriteFile returned %+v\n", err)
	}

}

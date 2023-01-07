package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	errors "git.sequentialread.com/forest/pkg-errors"
)

type MatrixAdmin struct {
	Client http.Client
	URL    string
	Token  string
}

type DeleteRoomRequest struct {
	Block      bool   `json:"block"`
	ForcePurge bool   `json:"force_purge"`
	Purge      bool   `json:"purge"`
	Message    string `json:"message"`
}

type DeleteRoomResponse struct {
	DeleteId string `json:"delete_id"`
}

type RoomDeletionStatusResponse struct {
	Results []RoomDeletionStatus `json:"results"`
}
type RoomDeletionStatus struct {
	DeleteId     string       `json:"delete_id"`
	Status       string       `json:"status"`
	Error        string       `json:"error"`
	ShutdownRoom ShutdownRoom `json:"shutdown_room"`
}
type ShutdownRoom struct {
	KickedUsers       []string `json:"kicked_users"`
	FailedToKickUsers []string `json:"failed_to_kick_users"`
	LocalAliases      []string `json:"local_aliases"`
	NewRoomId         string   `json:"new_room_id"`
}

func initMatrixAdmin(config *Config) *MatrixAdmin {

	return &MatrixAdmin{
		Client: http.Client{
			Timeout: 10 * time.Second,
		},
		URL:   config.MatrixURL,
		Token: config.MatrixAdminToken,
	}
}

// curl -H "Content-Type: application/json" -X DELETE "localhost:8008/_synapse/admin/v2/rooms/$roomid?access_token=xxxxxxxxx" \
// 	--data '{ "block": false, "force_purge": true, "purge": true, "message": "This room is being cleaned, stand by..." }'

func (admin *MatrixAdmin) DeleteRoom(roomId string) error {

	deleteRequestBodyObject := DeleteRoomRequest{
		Block:      false,
		ForcePurge: true,
		Purge:      true,
		Message:    "This room is being cleaned, stand by...",
	}
	deleteRequestBody, err := json.Marshal(deleteRequestBodyObject)
	if err != nil {
		return errors.Wrapf(err, "matrixAdmin.DeleteRoom('%s') cannot serialize deleteRequestBodyObject as JSON", roomId)
	}
	deleteURLWithoutToken := fmt.Sprintf("%s/_synapse/admin/v2/rooms/%s?access_token=", admin.URL, roomId)
	deleteURL := fmt.Sprintf("%s%s", deleteURLWithoutToken, admin.Token)
	deleteRequest, err := http.NewRequest("DELETE", deleteURL, bytes.NewBuffer(deleteRequestBody))

	if err != nil {
		return errors.Wrapf(err, "matrixAdmin.DeleteRoom('%s') cannot create deleteRequest", roomId)
	}

	deleteResponse, err := admin.Client.Do(deleteRequest)

	if err != nil {
		return errors.New(fmt.Sprintf(
			"HTTP DELETE %sxxxxxxx: %s",
			deleteURLWithoutToken, err.Error(),
		))
	}

	if deleteResponse.StatusCode >= 300 {
		responseBodyString := "read error"
		responseBody, err := ioutil.ReadAll(deleteResponse.Body)
		if err == nil {
			responseBodyString = string(responseBody)
		}

		return errors.New(fmt.Sprintf(
			"HTTP DELETE %sxxxxxxx: HTTP %d: %s",
			deleteURLWithoutToken, deleteResponse.StatusCode, responseBodyString,
		))
	}

	return nil
}

func (admin *MatrixAdmin) GetDeleteRoomStatus(roomId string) (string, []string, error) {

	statusURLWithoutToken := fmt.Sprintf("%s/_synapse/admin/v2/rooms/%s/delete_status?access_token=", admin.URL, roomId)
	statusURL := fmt.Sprintf("%s%s", statusURLWithoutToken, admin.Token)

	statusResponse, err := admin.Client.Get(statusURL)

	if err != nil {
		return "", nil, errors.New(fmt.Sprintf("HTTP GET %sxxxxxxx: %s", statusURLWithoutToken, err.Error()))
	}

	if statusResponse.StatusCode >= 300 {
		responseBodyString := "read error"
		responseBody, err := ioutil.ReadAll(statusResponse.Body)
		if err == nil {
			responseBodyString = string(responseBody)
		}

		return "", nil, errors.New(fmt.Sprintf(
			"HTTP GET %sxxxxxxx: HTTP %d: %s",
			statusURLWithoutToken, statusResponse.StatusCode, responseBodyString,
		))
	}

	responseBody, err := ioutil.ReadAll(statusResponse.Body)
	if err != nil {
		return "", nil, errors.New(fmt.Sprintf("HTTP GET %sxxxxxxx: read error: %s", statusURLWithoutToken, err.Error()))
	}
	var responseObject RoomDeletionStatusResponse
	err = json.Unmarshal(responseBody, &responseObject)
	if err != nil {
		return "", nil, errors.New(fmt.Sprintf("HTTP GET %sxxxxxxx: json parse error: %s", statusURLWithoutToken, err.Error()))
	}

	users := map[string]bool{}
	errorsSet := map[string]bool{}
	mostCompleteStatus := ""

	for _, result := range responseObject.Results {
		for _, userid := range result.ShutdownRoom.KickedUsers {
			users[userid] = true
		}
		for _, userid := range result.ShutdownRoom.FailedToKickUsers {
			users[userid] = true
		}
		if result.Error != "" {
			errorsSet[result.Error] = true
		}
		if result.Status == "shutting_down" {
			if mostCompleteStatus == "" {
				mostCompleteStatus = result.Status
			}
		} else if result.Status == "purging" {
			if mostCompleteStatus == "" || mostCompleteStatus == "shutting_down" {
				mostCompleteStatus = result.Status
			}
		} else if result.Status == "failed" {
			if mostCompleteStatus == "" || mostCompleteStatus == "shutting_down" || mostCompleteStatus == "purging" {
				mostCompleteStatus = result.Status
			}
		} else if result.Status == "complete" {
			if mostCompleteStatus == "" || mostCompleteStatus == "shutting_down" || mostCompleteStatus == "purging" || mostCompleteStatus == "failed" {
				mostCompleteStatus = result.Status
			}
		}

	}
	usersSlice := []string{}
	for userid := range users {
		usersSlice = append(usersSlice, userid)
	}

	if mostCompleteStatus == "failed" {
		errorsSlice := []string{}
		for errString := range errorsSet {
			errorsSlice = append(errorsSlice, errString)
		}

		return "", nil, errors.New(fmt.Sprintf("room deletion failed: \n%s", strings.Join(errorsSlice, "\n")))
	}

	return mostCompleteStatus, usersSlice, nil
}

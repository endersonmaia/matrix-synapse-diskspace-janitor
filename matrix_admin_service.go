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
	Client                   http.Client
	AdminMatrixRoomId        string
	MatrixServerPublicDomain string
	URL                      string
	Token                    string
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

type LoginRequestBody struct {
	Identifier        LoginIdentifier `json:"identifier"`
	DeviceDisplayName string          `json:"initial_device_display_name"`
	Password          string          `json:"password"`
	Type              string          `json:"type"`
}

type LoginIdentifier struct {
	Type string `json:"type"`
	User string `json:"user"`
}

type LoginResponseBody struct {
	AccessToken string `json:"access_token"`
}

type RoomMembersResponseBody struct {
	Members []string `json:"members"`
}

type RoomDetails struct {
	Name           string `json:"name"`
	CanonicalAlias string `json:"canonical_alias"`
}

func initMatrixAdmin(config *Config) *MatrixAdmin {

	return &MatrixAdmin{
		Client: http.Client{
			Timeout: 10 * time.Second,
		},
		AdminMatrixRoomId:        config.AdminMatrixRoomId,
		MatrixServerPublicDomain: config.MatrixServerPublicDomain,
		URL:                      config.MatrixURL,
		Token:                    config.MatrixAdminToken,
	}
}

// curl -H "Content-Type: application/json" -X DELETE "localhost:8008/_synapse/admin/v2/rooms/$roomid?access_token=xxxxxxxxx" \
// 	--data '{ "block": false, "force_purge": true, "purge": true, "message": "This room is being cleaned, stand by..." }'

func (admin *MatrixAdmin) DeleteRoom(roomId string, ban bool) error {

	deleteRequestBodyObject := DeleteRoomRequest{
		Block:      ban,
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

func (admin *MatrixAdmin) GetRoomName(roomId string) (string, error) {

	urlWithoutToken := fmt.Sprintf(
		"%s/_synapse/admin/v1/rooms/%s?access_token=",
		admin.URL, roomId,
	)
	url := fmt.Sprintf("%s%s", urlWithoutToken, admin.Token)
	response, err := admin.Client.Get(url)
	if err != nil {
		return "", errors.Wrapf(err, "HTTP GET %sxxxxxxx", urlWithoutToken)
	}

	responseBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", errors.Wrapf(err, "HTTP GET %sxxxxxxx read error", urlWithoutToken)
	}

	if response.StatusCode == 404 {
		return "no name found", nil
	}

	if response.StatusCode != 200 {
		return "", fmt.Errorf(
			"HTTP GET %sxxxxxxx: HTTP %d: %s",
			urlWithoutToken, response.StatusCode, string(responseBody),
		)
	}

	var responseObject RoomDetails
	err = json.Unmarshal(responseBody, &responseObject)
	if err != nil {
		return "", errors.Wrapf(err, "HTTP GET %sxxxxxxxxx response json parse error", urlWithoutToken)
	}

	if responseObject.CanonicalAlias != "" {
		return responseObject.CanonicalAlias, nil
	}
	if responseObject.Name != "" {
		return responseObject.Name, nil
	}
	return "no name found", nil
}

// curl 'https://matrix.cyberia.club/_matrix/client/r0/login' -X POST  -H 'Accept: application/json' -H 'content-type: application/json'
//  --data-raw '{"type":"m.login.password","password":"xxxxxxxxx","identifier":{"type":"m.id.user","user":"forestjohnson"},"initial_device_display_name":"chat.cyberia.club (Firefox, Ubuntu)"}'

func (admin *MatrixAdmin) Login(username, password string) (bool, error) {

	loginURL := fmt.Sprintf("%s/_matrix/client/v3/login", admin.URL)

	loginRequestBodyObject := LoginRequestBody{
		Identifier: LoginIdentifier{
			Type: "m.id.user",
			User: username,
		},
		DeviceDisplayName: "matrix-synapse-diskspace-janitor",
		Password:          password,
		Type:              "m.login.password",
	}
	loginRequestBody, err := json.Marshal(loginRequestBodyObject)
	if err != nil {
		return false, errors.Wrap(err, "can't serialize LoginRequestBody to json")
	}
	loginResponse, err := admin.Client.Post(loginURL, "application/json", bytes.NewBuffer(loginRequestBody))
	if err != nil {
		return false, err
	}

	//log.Printf("%s loginResponse.StatusCode: %d\n", username, loginResponse.StatusCode)

	if loginResponse.StatusCode > 200 {
		return false, nil
	}

	responseBody, err := ioutil.ReadAll(loginResponse.Body)
	if err != nil {
		return false, errors.Wrapf(err, "HTTP POST %s read error", loginURL)
	}
	var responseObject LoginResponseBody
	err = json.Unmarshal(responseBody, &responseObject)
	if err != nil {
		return false, errors.Wrapf(err, "HTTP POST %s response json parse error", loginURL)
	}

	logoutURL := fmt.Sprintf("%s/_matrix/client/v3/logout", admin.URL)
	logoutRequest, err := http.NewRequest("POST", logoutURL, nil)
	if err != nil {
		return false, errors.Wrap(err, "matrixAdmin.Login(...) cannot create logoutRequest")
	}

	logoutRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", responseObject.AccessToken))

	_, err = admin.Client.Do(logoutRequest)
	if err != nil {
		return false, err
	}

	roomMembersURLWithoutToken := fmt.Sprintf(
		"%s/_synapse/admin/v1/rooms/%s/members?access_token=",
		admin.URL, admin.AdminMatrixRoomId,
	)
	roomMembersURL := fmt.Sprintf("%s%s", roomMembersURLWithoutToken, admin.Token)
	roomMembersResponse, err := admin.Client.Get(roomMembersURL)
	if err != nil {
		return false, errors.Wrapf(err, "HTTP GET %sxxxxxxx", roomMembersURLWithoutToken)
	}

	roomMembersResponseBody, err := ioutil.ReadAll(roomMembersResponse.Body)
	if err != nil {
		return false, errors.Wrapf(err, "HTTP GET %sxxxxxxx read error", roomMembersURLWithoutToken)
	}

	if roomMembersResponse.StatusCode != 200 {
		return false, fmt.Errorf(
			"HTTP GET %sxxxxxxx: HTTP %d: %s",
			roomMembersURLWithoutToken, roomMembersResponse.StatusCode, string(roomMembersResponseBody),
		)
	}

	var roomMembersResponseObject RoomMembersResponseBody
	err = json.Unmarshal(roomMembersResponseBody, &roomMembersResponseObject)
	if err != nil {
		return false, errors.Wrapf(err, "HTTP GET %sxxxxxxxxx response json parse error", roomMembersURLWithoutToken)
	}
	if len(roomMembersResponseObject.Members) == 0 {
		return false, errors.Wrapf(err, "%s room did not have any members", admin.AdminMatrixRoomId)
	}

	for _, member := range roomMembersResponseObject.Members {
		//log.Printf("%s == %s\n", member, fmt.Sprintf("@%s:%s", username, admin.MatrixServerPublicDomain))
		if member == fmt.Sprintf("@%s:%s", username, admin.MatrixServerPublicDomain) {
			return true, nil
		}
	}

	return false, nil
}

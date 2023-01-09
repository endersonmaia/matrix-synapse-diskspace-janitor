package main

import (
	"encoding/json"
	"os"

	errors "git.sequentialread.com/forest/pkg-errors"
)

func WriteJsonFile[T any](path string, object T) error {
	mutex.Lock()
	defer mutex.Unlock()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewEncoder(file).Encode(object)
}

func ReadJsonFile[T any](path string) (T, error) {
	mutex.Lock()
	defer mutex.Unlock()
	var object T
	file, err := os.OpenFile(path, os.O_RDONLY, 0644)
	if err != nil && os.IsNotExist(err) {
		return object, nil
	}
	if err != nil {
		return object, err
	}
	defer file.Close()

	err = json.NewDecoder(file).Decode(&object)
	if err != nil {
		return object, errors.Wrapf(err, "json parse error on %s", path)
	}
	return object, nil
}

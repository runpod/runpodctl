package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func GetPublicSSHKey() (key string, err error) {
	input := Input{
		Query: `
		query myself {
			myself {
				id
				pubKey
			}
		}
		`,
	}
	res, err := Query(input)
	if err != nil {
		return "", err
	}
	if res.StatusCode != 200 {
		err = fmt.Errorf("statuscode %d", res.StatusCode)
		return
	}
	defer res.Body.Close()
	rawData, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	data := &UserOut{}
	if err = json.Unmarshal(rawData, data); err != nil {
		return "", err
	}
	if len(data.Errors) > 0 {
		err = errors.New(data.Errors[0].Message)
		return "", err
	}
	if data == nil || data.Data == nil || data.Data.Myself == nil {
		err = fmt.Errorf("data is nil: %s", string(rawData))
		return "", err
	}
	return data.Data.Myself.PubKey, nil
}

func AddPublicSSHKey(key []byte) error {
	//pull existing pubKey
	existingKeys, err := GetPublicSSHKey()
	if err != nil {
		return err
	}
	keyStr := string(key)
	//check for key present
	if strings.Contains(existingKeys, keyStr) {
		return nil
	}
	//	concat key onto pubKey
	newKeys := existingKeys + "\n\n" + keyStr
	//	set new pubKey
	input := Input{
		Query: `
		mutation Mutation($input: UpdateUserSettingsInput) {
			updateUserSettings(input: $input) {
			  id
			}
		  }
		`,
		Variables: map[string]interface{}{"input": map[string]interface{}{"pubKey": newKeys}},
	}
	_, err = Query(input)
	if err != nil {
		return err
	}
	return nil
}

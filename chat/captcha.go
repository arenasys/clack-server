package chat

import (
	. "clack/common"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	hCaptchEndpoint = "https://hcaptcha.com/siteverify"
)

var usedCaptchas = map[string]int64{}
var usedCaptchasMutex = &sync.Mutex{}
var usedCaptchasLastCleaned = time.Now().UnixMilli()

type hCaptchaResult struct {
	Success   bool   `json:"success"`
	Timestamp string `json:"challenge_ts"`
	Hostname  string `json:"hostname"`
}

func hCaptchaVerify(response string, client_ip string, site_key string, secret_key string) (bool, error) {
	alreadyUsed := false

	usedCaptchasMutex.Lock()

	now := time.Now().UnixMilli()
	const fiveMinutes = int64(5 * 60 * 1000)
	if usedCaptchasLastCleaned+(fiveMinutes*5) < now {
		for response, timestamp := range usedCaptchas {
			if now-timestamp > fiveMinutes {
				delete(usedCaptchas, response)
			}
		}
	}

	if _, ok := usedCaptchas[response]; ok {
		alreadyUsed = true
	} else {
		usedCaptchas[response] = time.Now().UnixMilli()
	}

	usedCaptchasMutex.Unlock()

	if alreadyUsed {
		return false, NewError(ErrorCodeInvalidCaptcha, nil)
	}

	//data := fmt.Sprintf("secret=%s&sitekey=%s&remoteip=%s&response=%s", secret_key, site_key, client_ip, response)
	data := fmt.Sprintf("secret=%s&sitekey=%s&response=%s", secret_key, site_key, response)
	req, err := http.NewRequest("POST", hCaptchEndpoint, strings.NewReader(data))
	if err != nil {
		fmt.Println("Error creating request:", err)
		return false, NewError(ErrorCodeInternalError, err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", UserAgent)
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		fmt.Println("Error making request:", err)
		return false, NewError(ErrorCodeInternalError, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Println("Error: received non-200 response code:", resp.StatusCode)
		return false, NewError(ErrorCodeInternalError, nil)
	}

	var result hCaptchaResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Println("Error decoding response:", err)
		return false, NewError(ErrorCodeInternalError, err)
	}

	if !result.Success {
		fmt.Println("Captcha verification failed")
		return false, NewError(ErrorCodeInvalidCaptcha, nil)
	}

	return true, nil
}

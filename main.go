package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func getWebhookURL() string {
	if len(os.Getenv("discord")) > 0 {
		return os.Getenv("discord")
	}
	log.Fatalf("No discord webhook in environment variable!")
	return ""
}

// translate appid to game name
func getGameName(appid int, responseBytes []byte) string {
	var respJson appIDTranslator
	unMarshErr := json.Unmarshal(responseBytes, &respJson)
	if unMarshErr != nil {
		log.Fatalf("Could not parse GetAppList. Error: %v", unMarshErr)
	}

	for _, game := range respJson.Applist.Apps {
		if game.Appid == appid {
			return game.Name
		}
	}
	failMessage := fmt.Sprintf("%v not found!", appid)
	return failMessage
}

type appIDTranslator struct {
	Applist struct {
		Apps []struct {
			Appid int    `json:"appid"`
			Name  string `json:"name"`
		}
	}
}

// generic HTTP POST to whatever URL you give it
func getAPIContent(url string) []byte {
	log.Printf("Performing GET request to %v...\n", url)

	// create HTTP request with specific headers
	req, reqErr := http.NewRequest("GET", url, nil)
	if reqErr != nil {
		log.Fatalf("Could not form HTTP Request. Error: %v\n", reqErr)
	}
	req.Header.Add("user-agent", "steam news notifier")

	// create a HTTP client with a 5s timeout
	client := http.Client{Timeout: 5 * time.Second}
	resp, getErr := client.Do(req) // send the request
	if getErr != nil {
		log.Fatalf("Could not perform HTTP GET to %v. Error: %v\n", url, getErr)
	}

	// basic HTTP code handling and load the response body into the buffer
	if resp.StatusCode == http.StatusTooManyRequests {
		log.Println("Received a HTTP 429 response. Sleeping for 10s!")
		time.Sleep(10 * time.Second)
		getAPIContent(url)

	} else if resp.StatusCode != http.StatusOK {
		log.Fatalf("Received an error from %v. Exiting...\nError: %v\nBody: %v\n", url, resp.Status, resp.Body)

	} else {
		body, readErr := ioutil.ReadAll(resp.Body)
		if readErr != nil {
			postToDiscord(fmt.Sprintf("Encountered and error reading response from %v", url))
		}
		return body
	}
	return []byte{}

}

type discordText struct {
	Content string `json:"content"`
}

// format string for discord notification
func formatBuildMessage(name string) string {
	return ""
}

// post string returned from formatNewsMessage() to discord
func postToDiscord(content string) {
	// set webhook URL from env vars
	webhookURL := getWebhookURL()

	// parse message into json struct for the payload
	payload := discordText{Content: content}
	jsonContent, marshErr := json.Marshal(&payload)
	if marshErr != nil {
		log.Fatalf("Could not marshal message. Error: %v", marshErr)
	}

	// form HTTP request
	req, reqErr := http.NewRequest("POST", webhookURL, bytes.NewBuffer(jsonContent))
	if reqErr != nil {
		log.Fatalf("Could not make HTTP Request for Discord. Error: %v", reqErr)
	}
	req.Header.Add("content-type", "application/json")

	// create HTTP client and do the request
	client := http.Client{Timeout: 5 * time.Second}
	response, respErr := client.Do(req)
	if respErr != nil {
		log.Fatalf("Could not form HTTP client. Error: %v", respErr)
	}

	// handle status codes that we dont care about
	if response.StatusCode != http.StatusNoContent {
		log.Printf("HTTP POST to Discord server failed. Status Code: %v", response.StatusCode)
		body, readErr := ioutil.ReadAll(response.Body)
		if readErr != nil {
			log.Fatalf("Could not read response body. Error: %v", readErr)
		}
		log.Fatal(string(body))
	} else {
		fmt.Println("Success!")
	}
}

func isSteamCMDInstalled() bool {
	_, stErr := os.Stat("steamcmd.sh")
	if os.IsNotExist(stErr) {
		log.Println("Did not find SteamCMD in the current dir. Please install it before proceeding")
		return false
	}

	log.Println("Found SteamCMD in the current directory... proceeding")
	return true
}

// if steamcmd is installed. get the build IDs and return them as a slice
func getAppBuildInfo(appid int) ([]string, error) {
	resp, respErr := getAppIDInfo(appid)
	if respErr != nil {
		return nil, errors.New(fmt.Sprintf("error getting build info. error: %v", respErr))
	}

	// TODO: fix rate limit detection
	//if bytes.ContainsAny(resp, `Rate Limit Exceeded`) {
	//	return nil, errors.New("exceeded steamcmd rate limit")
	//}

	branchPos := bytes.Index(resp, []byte("buildid"))

	length := len(string(resp)) - 1
	tmpString := string(resp[branchPos:length])

	remQuotes := strings.ReplaceAll(tmpString, "\"", "")
	remTabs := strings.ReplaceAll(remQuotes, "\t", "")

	// simple regex to obtain only the build IDs but not epoch time
	re, compErr := regexp.Compile("\\d{4,12}")
	if compErr != nil {
		return nil, errors.New(fmt.Sprintf("regex compile error. error: %v", compErr))
	}

	// fmt.Println(remTabs)

	match := re.FindAllString(remTabs, 6)

	if len(match) < 1 {
		return nil, errors.New("could not find any build information from steamcmd")
	}
	return match, nil
}

// convert each build id into int and add to map
func parseBuildSlice(buildInfo []string) (map[string]map[string]map[string]interface{}, error) {
	// TODO: implement index out of range checks and handle appropriately
		pubTime, parseErr := strconv.ParseInt(buildInfo[1], 10, 64)
		if parseErr != nil {
			return nil, errors.New(fmt.Sprintf("could not convert public build timestamp to int64.\nerror: %v", parseErr))
		}

		betaTime, parseErr := strconv.ParseInt(buildInfo[3], 10, 64)
		if parseErr != nil {
			return nil, errors.New(fmt.Sprintf("could not convert public build timestamp to int64.\nerror: %v", parseErr))
		}

		privateTime, parseErr := strconv.ParseInt(buildInfo[5], 10, 64)
		if parseErr != nil {
			return nil, errors.New(fmt.Sprintf("could not convert public build timestamp to int64.\nerror: %v", parseErr))
	}

	builds := map[string]map[string]map[string]interface{}{"buildInfo": {"public": {"buildid": buildInfo[0], "timestamp": pubTime},
		"beta":    {"buildid": buildInfo[2], "timestamp": betaTime},
		"private": {"buildid": buildInfo[4], "timestamp": privateTime}}}

	return builds, nil
}

func getAppIDInfo(appid int) ([]byte, error) {
	appInfoRequest := fmt.Sprintf("+app_info_request %v", appid)
	appInfoPrint := fmt.Sprintf("+app_info_print %v", appid)
	outBytes, err := exec.Command("./steamcmd.sh", "+login anonymous", appInfoRequest, appInfoPrint, "+exit").Output()
	return outBytes, err
}

func saveBuildInfo(builds map[string]map[string]map[string]interface{}) (bool, error){
	_, err := os.Stat("builds.txt")
	if os.IsNotExist(err) {
		_, _ = os.Create("builds.txt")
	}
	file, openErr := os.OpenFile("builds.txt", os.O_WRONLY|os.O_APPEND, 0666)
	if openErr != nil {
		return false, errors.New(fmt.Sprintf("could not open builds.txt\nerror: %v", openErr))
	}

	return true
}

func getBuilds(appid int) {
	if isSteamCMDInstalled() {
		buildIDs, err := getAppBuildInfo(appid)
		checkErr(err)

		builds, err := parseBuildSlice(buildIDs)
		checkErr(err)
		fmt.Println(builds["buildInfo"]["public"])
	}

}

func checkErr(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

func main() {
	// get list of game names/appids
	// gameNameBytes := getAPIContent("https://api.steampowered.com/ISteamApps/GetAppList/v2/")

	// check for a new steam news post for a list of appids
	/*
		for {
			appIDs := []int{717790}
			for _, appid := range appIDs {
				// name := getGameName(appid, gameNameBytes)
				fmt.Println(appid)
			}
	*/
	getBuilds(717790)

	//	log.Println("Sleeping for 15m...")
	//	time.Sleep(15 * time.Minute)
}

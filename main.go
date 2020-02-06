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
			postToDiscord(fmt.Sprintf("Encountered an error reading response from %v", url))
		}
		return body
	}
	return []byte{}
}

type discordText struct {
	Content string `json:"content"`
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

// get appid info from steamcmd
func getAppIDInfo(appid int) ([]byte, error) {
	appInfoRequest := fmt.Sprintf("+app_info_request %v", appid)
	appInfoPrint := fmt.Sprintf("+app_info_print %v", appid)
	outBytes, err := exec.Command("./steamcmd.sh", "+login anonymous", appInfoRequest, appInfoPrint, "+exit").Output()
	return outBytes, err
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
	re, compErr := regexp.Compile("timeupdated(\\d{6,12})")
	if compErr != nil {
		return nil, errors.New(fmt.Sprintf("regex compile error. error: %v", compErr))
	}

	// fmt.Println(remTabs)

	match := re.FindAllStringSubmatch(remTabs, 6)

	if len(match) < 1 {
		return nil, errors.New("could not find any build information from steamcmd")
	}

	// create a new slice containing only the epoch strings from each build's timestamp
	var matchedTimes []string
	for _, group := range match {

		matchedTimes = append(matchedTimes, group[1])
	}
	return matchedTimes, nil
}

// check build timestamps to determine if a build was updated
func checkBuildTime(timeSlice []string) (string, error) {
	// iterate through slice of epoch strings and convert them to int64
	for i, timeStr := range timeSlice {
		buildTime, convErr := strconv.ParseInt(timeStr, 10, 64)
		if convErr != nil {
			return "nil", convErr
		}

		// if time since is < 1h, return which build was updated
		if time.Since(time.Unix(buildTime, 0)).Hours() < 1 {
			switch {
			case i == 0:
				return "public", nil
			case i == 1:
				return "beta", nil
			case i == 2:
				return "private", nil
			}
		}
		i++
	}
	return "", nil
}

func getBuilds(appid int, gameNameBytes []byte) {

			gameName := getGameName(appid, gameNameBytes)

			buildTimes, err := getAppBuildInfo(appid)
			checkErr(err)

			build, timeErr := checkBuildTime(buildTimes)
			checkErr(timeErr)
			if len(build) > 0 {
				postToDiscord(fmt.Sprintf("%s's %v branch has a new build!", gameName, build))
			} else {
				log.Printf("No new build detected for %v\n", gameName)
			}
}

func checkErr(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

func main() {
	// get list of game names/appids
	gameNameBytes := getAPIContent("https://api.steampowered.com/ISteamApps/GetAppList/v2/")

	// check for a new steam news post for a list of appids
	if isSteamCMDInstalled() {
				getBuilds(717790, gameNameBytes)
			}
		}

		//	log.Println("Sleeping for 15m...")
		//	time.Sleep(15 * time.Minute)



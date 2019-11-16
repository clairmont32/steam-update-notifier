package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"
)

func getWebhookURL() string {
	if len(os.Getenv("discord")) > 0 {
		return os.Getenv("discord")
	}
	log.Fatalf("No discord webhook in environment variable!")
	return ""
}

func readConfig() {
	configFile, openErr := os.Open("config.yaml")
	if openErr != nil {
		log.Fatalf("Could not open config.yaml. Error: %v", openErr)
	}

	defer configFile.Close()
	scanner := bufio.NewScanner(configFile)
	var configContents []string
	for scanner.Scan() {
		configContents = append(configContents, scanner.Text())
	}
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

func saveNewsGid(gid string) {
	file, openErr := os.OpenFile("news_gid.txt", os.O_CREATE|os.O_RDWR|os.O_APPEND, 0666)
	if openErr != nil {
		log.Fatalf("Could not open news_gid.txt. Error: %v", openErr)
	}
	defer file.Close()

	log.Println(gid)
	n, writeErr := file.WriteString(gid + "\n")
	if writeErr != nil {
		log.Fatalf("Could not write GID to news_gid.txt")
	}
	if n < 1 {
		log.Fatal("Did not write more than 1 byte to news_gid.txt")
	}

	log.Printf("GID %v written to file", gid)
}

func readNewsGid() []byte {
	gids, openErr := ioutil.ReadFile("news_gid.txt")
	if openErr != nil {
		log.Fatalf("Could not read from news_gid.txt. Error: %v", openErr)
	}
	return gids
}

// generic HTTP POST to whatever URL you give it
func getAPIContent(url string) []byte {
	log.Printf("Performing GET request to %v...\n", url)

	// create HTTP request with specific headers
	req, reqErr := http.NewRequest("GET", url, nil)
	if reqErr != nil {
		log.Fatalf("Could not form HTTP Request. Error: %v\n", reqErr)
	}
	req.Header.Add("user-agent", "matthew.clairmont1@gmail.com's app update notifier")

	// create a HTTP client with a 5s timeout
	client := http.Client{Timeout: 5 * time.Second}
	resp, getErr := client.Do(req) // send the request
	if getErr != nil {
		log.Fatalf("Could not perform HTTP GET to %v. Error: %v\n", url, getErr)
	}

	// basic HTTP code handling and load the response body into the buffer
	if resp.StatusCode == http.StatusTooManyRequests {
		log.Println("Received a HTTP 429 response. Sleepin`g for 10s!")
		time.Sleep(10 * time.Second)
		getAPIContent(url)

	} else if resp.StatusCode != http.StatusOK {
		log.Fatalf("Received an error from %v. Exiting...\nError: %v\nBody: %v\n", url, resp.Status, resp.Body)

	} else {
		body, readErr := ioutil.ReadAll(resp.Body)
		if readErr != nil {
			log.Fatalf("Could not ReadAll from resp.Body. Error: %v", readErr)
		}
		return body
	}
	return []byte{}

}

type newsResponse struct {
	AppNews struct {
		AppID     int `json:"appid"`
		NewsItems []struct {
			Gid    string `json:"gid"`
			Title  string `json:"title"`
			Date   int64  `json:"date"`
			URL    string `json:"url"`
			Author string `json:"author"`
		} `json:"newsitems"`
	}
}

type discordText struct {
	Content string `json:"content"`
}

func checkIfDateWithinHour(date int64) bool {
	now := time.Now().Unix()
	timeDiff := date - now
	if timeDiff > 3600 {
		return true
	}
	return false
}

// format string for discord notification
func formatNewsMessage(content newsResponse, name string) string {
	var messageString string
	for _, item := range content.AppNews.NewsItems {
		messageString = fmt.Sprintf("New news post detected for %v\n%v\n%v", name, item.Title, item.URL)
	}
	return messageString
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

func getSteamNews(gidMap map[string]string, appid int) {

	if len(gidMap) < 0 {
		savedGids := readNewsGid()
		for _, gid := range savedGids {
			gidMap[string(gid)] = ""
		}
	}

	url := fmt.Sprintf("https://api.steampowered.com/ISteamNews/GetNewsForApp/v2/?appid=%v&count=1", appid)
	data := getAPIContent(url)
	var steamResponse newsResponse
	jsonErr := json.Unmarshal(data, &steamResponse)
	if jsonErr != nil {
		log.Fatalf("Could not process API response. Error: %v", jsonErr)
	}

	// debug print
	// fmt.Println(steamResponse.AppNews.NewsItems)

	// check if each news GID is in the map
	// if not, add it and save to file in case the service dies for some reason
	for _, item := range steamResponse.AppNews.NewsItems {

		// primarily for startup, check if new posts within 1 hour so you dont
		// spam the discord channel with the last news post. unless this is offline for several days, they're old anyway
		if checkIfDateWithinHour(item.Date) {
			// check if the gid's are loading into memory already
			if _, ok := gidMap[item.Gid]; !ok {
				gidMap[item.Gid] = ""
				saveNewsGid(item.Gid)
				// get game name, format message, send to discord
				nameBytes := getAPIContent("https://api.steampowered.com/ISteamApps/GetAppList/v2/")
				name := getGameName(appid, nameBytes)
				fmt.Println(name)
				// postToDiscord(formatNewsMessage(steamResponse, name))
			} else {
				log.Println("Nothing new found")

			}
		} else {
			log.Println("Nothing new found in last hour")
		}
	}
}

func installSteamCMD() bool {
	_, stErr := os.Stat("steamcmd.sh")
	if os.IsNotExist(stErr) {
		log.Println("Did not find SteamCMD in the current dir. Installing now...")
		installerCmd := fmt.Sprint("curl -sqL 'https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz' | tar zxvf -")

		execErr := exec.Command(installerCmd).Run()
		if execErr != nil {
			log.Printf("Encountered an issue installing SteamCMD. Please install it manually with '%v'\n", installerCmd)
			log.Fatalf("Error: %v\n", execErr)
		}
		// call this function again to ensure its installed
		installSteamCMD()

	}
	log.Println("Found steamcmd.sh in the current dir; continuing...")
	return true
}

func getAppInfo(appid int) []byte {
	if installSteamCMD() {
		requestInfo := fmt.Sprintf("./steamcmd.sh +login anonymous +app_info_request %v +exit", appid)
		_, reqErr := exec.Command(requestInfo).Output()
		if reqErr != nil {
			log.Fatalf("Encountered and error requesting app info. Error: %v", reqErr)
		}
		log.Println("App info requested successfully!")

		printInfo := fmt.Sprintf("./steamcmd.sh +login anonymous +app_info_print %v +exit", appid)
		infoByte, infoErr := exec.Command(printInfo).Output()
		if infoErr != nil {
			log.Fatalf("Encountered an error obtaining app information. Error: %v", infoErr)
		}
		log.Println("App info obtained.")

		return infoByte
	}

	log.Fatalf("Encountered an unexpected issue interacting with SteamCMD.")
	return []byte{}
}

func main() {
	// ensure logfile exists, if not, create it.
	logfile := "news_updater.log"
	_, statErr := os.Stat(logfile)
	if os.IsNotExist(statErr) {
		file, crErr := os.Create(logfile)
		if crErr != nil {
			fmt.Printf("Could not create logfile. Error: %v", crErr)
		}
		_ = file.Close()
	}

	// check for a new steam news post for a list of appids
	gidMap := make(map[string]string)
	for {
		appIDs := []int{717790}
		for _, appid := range appIDs {
			getSteamNews(gidMap, appid) // use a new goroutine for steam news
			go getAppInfo(appid)
		}

		time.Sleep(15 * time.Minute)
	}

}

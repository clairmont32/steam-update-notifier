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
	file, openErr := os.OpenFile("news_gid.txt", os.O_CREATE|os.O_APPEND, 0666)
	if openErr != nil {
		log.Fatalf("Could not open news_gid.txt. Error: %v", openErr)
	}
	defer file.Close()

	log.Println(gid)
	n, writeErr := file.WriteString(gid)
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
	fmt.Printf("Performing GET request to %v...\n", url)

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
		fmt.Println("Received a HTTP 429 response. Sleepin`g for 10s!")
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
			Date   int    `json:"date"`
			URL    string `json:"url"`
			Author string `json:"author"`
		} `json:"newsitems"`
	}
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
	} else {
		fmt.Println("Success!")
	}
}

// format string for discord notification
func formatNewsMessage(content newsResponse, name string) string {
	var messageString string
	for _, item := range content.AppNews.NewsItems {
		messageString = fmt.Sprintf("New news post detected for %v\n%v\n%v", name, item.Title, item.URL)
	}
	return messageString
}

func getSteamNews() {
	appIDs := []int{717790}

	gidMap := make(map[string]string)
	if len(gidMap) < 0 {
		savedGids := readNewsGid()
		for _, gid := range savedGids {
			gidMap[string(gid)] = ""
		}
	}

	for _, appid := range appIDs {
		url := fmt.Sprintf("https://api.steampowered.com/ISteamNews/GetNewsForApp/v2/?appid=%v&count=1", appid)
		data := getAPIContent(url)
		var steamResponse newsResponse
		jsonErr := json.Unmarshal(data, &steamResponse)
		if jsonErr != nil {
			log.Fatalf("Could not process API response. Error: %v", jsonErr)
		}

		// check if each news GID is in the map
		// if not, add it and save to file in case the service dies for some reason
		for _, item := range steamResponse.AppNews.NewsItems {
			if _, ok := gidMap[item.Gid]; !ok {
				gidMap[item.Gid] = ""
				saveNewsGid(item.Gid)

				// get game name, format message, send to discord
				nameBytes := getAPIContent("https://api.steampowered.com/ISteamApps/GetAppList/v2/")
				name := getGameName(appid, nameBytes)
				postToDiscord(formatNewsMessage(steamResponse, name))
			}
		}

	}
}

func main() {
	file, createErr := os.Open("news_updater.txt")
	if createErr != nil {
		log.Fatalf("Could not create log file")
	}
	defer file.Close()
	log.SetOutput(file)

	for {
		getSteamNews()
		time.Sleep(1 * time.Hour)
	}

}

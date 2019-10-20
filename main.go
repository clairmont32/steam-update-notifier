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
	if len(os.Getenv("discord")) != 0 {
		return os.Getenv("discord")
	} else {
		log.Fatalf("No discord webhook in environment variable!")
	}
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

func getGameName() {
	// TODO reach out to steam API to lookup game title
}

func getNewsContent(url string) bytes.Buffer {
	fmt.Printf("Performing GET request to %v...\n", url)

	// create HTTP request with specific headers
	req, reqErr := http.NewRequest("GET", url, nil)
	if reqErr != nil {
		log.Fatalf("Could not form HTTP Request. Error: %v\n", reqErr)
	}
	req.Header.Add("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/77.0.3865.90 Safari/537.36")

	// create a HTTP client with a 5s timeout
	client := http.Client{Timeout: 5 * time.Second}
	resp, getErr := client.Do(req) // send the request
	if getErr != nil {
		log.Fatalf("Could not perform HTTP GET to %v. Error: %v\n", url, getErr)
	}

	// initialize a byte buffer to hold the response body
	var buffer bytes.Buffer

	// basic HTTP code handling and load the response body into the buffer
	if resp.StatusCode == http.StatusTooManyRequests {
		fmt.Println("Received a HTTP 429 response. Sleeping for 10s!")
		time.Sleep(10 * time.Second)
		getNewsContent(url)

	} else if resp.StatusCode != http.StatusOK {
		log.Fatalf("Received an error from %v. Exiting...\nError: %v\nBody: %v\n", url, resp.Status, resp.Body)

	} else {
		numRead, bufErr := buffer.ReadFrom(resp.Body)
		_ = resp.Body.Close()
		if bufErr != nil && numRead < 1 {
			log.Fatalf("Could not load response body into content variable. Error: %v", bufErr)
		}
		fmt.Println("Successfully loaded the latest news article into into memory!")
		return buffer
	}
	return bytes.Buffer{}
}

type NewsResponse struct {
	AppNews struct {
		AppID     int `json:"appid"`
		NewsItems []struct {
			Title  string `json:"title"`
			Date   int    `json:"date"`
			Url    string `json:"url"`
			Author string `json:"author"`
		} `json:"newsitems"`
	}
}

type DiscordText struct {
	Content string `json:"content"`
}

func postToDiscord(content string) {
	// set webhook URL from env vars
	webhookURL := getWebhookURL()

	// parse message into json struct for the payload
	payload := DiscordText{Content: content}
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

func formatNewsMessage(content NewsResponse, appID int) string {
	var messageString string
	for _, item := range content.AppNews.NewsItems {
		messageString = fmt.Sprintf("New news post detected for %v\n%v\n%v", appID, item.Title, item.Url)
	}
	return messageString
}

func main() {
	appIDs := []int{717790, 107410, 552990}
	for _, appID := range appIDs {
		url := fmt.Sprintf("https://api.steampowered.com/ISteamNews/GetNewsForApp/v2/?appid=%v&count=1", appID)
		data := getNewsContent(url)
		var steamResponse NewsResponse
		jsonErr := json.Unmarshal(data.Bytes(), &steamResponse)
		if jsonErr != nil {
			log.Fatalf("Could not process API response. Error: %v", jsonErr)
		}
		postToDiscord(formatNewsMessage(steamResponse, appID))
	}
}

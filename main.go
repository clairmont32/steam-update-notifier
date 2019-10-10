package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

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
		if bufErr != nil && numRead < 1 {
			log.Fatalf("Could not load response body into content variable. Error: %v", bufErr)
		}
		fmt.Println("Successfully loaded the latest news article into into memory!")
		return buffer
	}
	return bytes.Buffer{}
}

type SteamResponse struct {
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

func main() {
	url := "https://api.steampowered.com/ISteamNews/GetNewsForApp/v2/?appid=717790&count=1"
	data := getNewsContent(url)
	var steamResponse SteamResponse
	jsonErr := json.Unmarshal(data.Bytes(), &steamResponse)
	if jsonErr != nil {
		log.Fatalf("Could not process API response. Error: %v", jsonErr)
	}
	// fmt.Println(data.String())
	for _, item := range steamResponse.AppNews.NewsItems {
		fmt.Println(item.Title)
	}
	}

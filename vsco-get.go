package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Noooste/azuretls-client"
	"github.com/schollz/progressbar/v3"
)

// ========== Configuration and Constants ==========
const (
	userAgent           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/115.0"
	authorizationToken  = "Bearer 7356455548d0a1d886db010883388d08be84d0c9"
	maxRetries          = 100
	retryDelay          = 10 * time.Second
	pageDelayMin        = 3
	pageDelayMax        = 5
)

var (
	baseURL = "https://vsco.co"
	apiURL  = "https://vsco.co/api"
)

// ========== HTTP Client with azuretls ==========
type HTTPClient struct {
	session *azuretls.Session
}

func NewHTTPClient() *HTTPClient {
	session := azuretls.NewSession()
	
	// Set Firefox browser fingerprint
	session.Browser = azuretls.Firefox
	
	// Set headers including authorization token and referrer
	session.OrderedHeaders = azuretls.OrderedHeaders{
		{"User-Agent", userAgent},
		{"Accept", "application/json, text/plain, */*"},
		{"Accept-Language", "en-US,en;q=0.5"},
		{"Authorization", authorizationToken},
		{"Referer", "https://vsco.co/"},
		{"Origin", baseURL},
		{"DNT", "1"},
		{"Connection", "keep-alive"},
		{"Sec-Fetch-Dest", "empty"},
		{"Sec-Fetch-Mode", "cors"},
		{"Sec-Fetch-Site", "same-origin"},
	}
	
	// Set timeout
	session.TimeOut = 30 * time.Second
	
	return &HTTPClient{
		session: session,
	}
}

func (c *HTTPClient) Close() {
	if c.session != nil {
		c.session.Close()
	}
}

func (c *HTTPClient) Get(url string) (*azuretls.Response, error) {
	return c.session.Get(url)
}

// GetWithRetry attempts a request with retries on failure
func (c *HTTPClient) GetWithRetry(url string) (*azuretls.Response, error) {
	var lastErr error
	var resp *azuretls.Response

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			fmt.Printf("\n⚠️  Rate limited? Retry attempt %d/%d in %v...\n", attempt, maxRetries, retryDelay)
			time.Sleep(retryDelay)
		}

		resp, lastErr = c.session.Get(url)
		if lastErr != nil {
			fmt.Printf("Request error: %v\n", lastErr)
			continue
		}

		// Check if response is HTML (rate limit page)
		if len(resp.Body) > 0 && resp.Body[0] == '<' {
			fmt.Printf("⚠️  Received HTML response (status %d) - possible rate limiting\n", resp.StatusCode)
			fmt.Printf("Response preview: %s\n", string(resp.Body)[:min(200, len(resp.Body))])
			lastErr = fmt.Errorf("received HTML instead of JSON")
			continue
		}

		// Check for 429 rate limit status code
		if resp.StatusCode == 429 {
			fmt.Printf("⚠️  Rate limit hit (status 429)\n")
			lastErr = fmt.Errorf("rate limited")
			continue
		}

		// Check for other non-200 status codes
		if resp.StatusCode != 200 {
			fmt.Printf("⚠️  Bad status code: %d\n", resp.StatusCode)
			lastErr = fmt.Errorf("bad status: %d", resp.StatusCode)
			continue
		}

		// Success
		return resp, nil
	}

	return nil, fmt.Errorf("failed after %d retries, last error: %v", maxRetries, lastErr)
}

// DownloadFile downloads a file without progress bar
func (c *HTTPClient) DownloadFile(url, filepath string) error {
	resp, err := c.session.Get(url)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("bad status: %d", resp.StatusCode)
	}

	return os.WriteFile(filepath, resp.Body, 0644)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// randomDelay sleeps for a random duration between min and max seconds
func randomDelay(minSec, maxSec int) {
	delay := time.Duration(rand.Intn(maxSec-minSec+1)+minSec) * time.Second
	fmt.Printf("⏳ Waiting %v before next page request...\n", delay)
	time.Sleep(delay)
}

// ========== Scraper Types ==========
type sitesResponse struct {
	Sites []struct {
		ID              int    `json:"id"`
		ProfileImage    string `json:"profile_image"`
		ProfileImageID  string `json:"profile_image_id"`
	} `json:"sites"`
}

type imageList struct {
	Media []Media `json:"media"`
	Page  int     `json:"page"`
	Size  int     `json:"size"`
	Total int     `json:"total"`
}

type Media struct {
	ID            string `json:"_id"`
	Is_video      bool   `json:"is_video"`
	Video_url     string `json:"video_url"`
	Responsive_url string `json:"responsive_url"`
	Upload_date   int64  `json:"upload_date"`
}

type Scraper struct {
	httpClient      *HTTPClient
	username        string
	numWorkers      int
	id              int
	profileImageID  string
	downloadDir     string
	completedChan   chan string
	progressBar     *progressbar.ProgressBar
}

// ========== Scraper Implementation ==========
func NewScraper(username string, numWorkers int) *Scraper {
	// Initialize random seed
	rand.Seed(time.Now().UnixNano())
	
	// Create downloads directory
	downloadDir := "downloads"
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		fmt.Printf("Warning: Could not create downloads directory: %v\n", err)
	}

	return &Scraper{
		httpClient:    NewHTTPClient(),
		username:      username,
		numWorkers:    numWorkers,
		downloadDir:   downloadDir,
		completedChan: make(chan string, 100),
	}
}

func (s *Scraper) Close() {
	if s.httpClient != nil {
		s.httpClient.Close()
	}
	close(s.completedChan)
}

func (s *Scraper) GetUserInfo() error {
	url := fmt.Sprintf("%s/2.0/sites?subdomain=%s", apiURL, s.username)

	resp, err := s.httpClient.GetWithRetry(url)
	if err != nil {
		return fmt.Errorf("failed getting user info for user %s: %w", s.username, err)
	}

	var body sitesResponse
	err = json.Unmarshal(resp.Body, &body)
	if err != nil {
		return fmt.Errorf("failed to decode JSON response for user info %s: %w\nResponse preview: %s", 
			s.username, err, string(resp.Body)[:min(200, len(resp.Body))])
	}

	if len(body.Sites) < 1 {
		return fmt.Errorf("expected site, got %d", len(body.Sites))
	}

	s.id = body.Sites[0].ID
	s.profileImageID = body.Sites[0].ProfileImageID

	fmt.Printf("Found user: %s (ID: %d, Profile Image ID: %s)\n", s.username, s.id, s.profileImageID)
	return nil
}

func (s *Scraper) fetchImageList() (imageList, error) {
	var list imageList
	page := 1

	for {
		url := fmt.Sprintf("%s/2.0/medias?site_id=%d&page=%d", apiURL, s.id, page)

		resp, err := s.httpClient.GetWithRetry(url)
		if err != nil {
			return imageList{}, fmt.Errorf("failed to get image list for user %s (page %d): %w", s.username, page, err)
		}

		var curPage imageList
		err = json.Unmarshal(resp.Body, &curPage)
		if err != nil {
			// Print the response preview for debugging
			preview := string(resp.Body)
			if len(preview) > 500 {
				preview = preview[:500] + "..."
			}
			return imageList{}, fmt.Errorf("failed to decode JSON for user %s page %d: %w\nResponse preview:\n%s", 
				s.username, page, err, preview)
		}

		list.Media = append(list.Media, curPage.Media...)
		list.Total = curPage.Total

		fmt.Printf("Fetched page %d, got %d items (total: %d)\n", page, len(curPage.Media), list.Total)

		if len(curPage.Media) == 0 {
			break
		}
		
		page++
		
		// Add random delay between page requests (3-5 seconds)
		if len(curPage.Media) > 0 {
			randomDelay(pageDelayMin, pageDelayMax)
		}
	}

	return list, nil
}

// vsco returns us links that doesn't have https:// in front of it
func fixUrl(rawUrl string) (fixedUrl string) {
	if strings.HasPrefix(rawUrl, "https://") {
		return rawUrl
	}
	return "https://" + rawUrl
}

func getCorrectUrl(media Media) (url string) {
	if media.Is_video {
		return media.Video_url
	}
	return media.Responsive_url
}

func getMediaFilename(media Media) (string, error) {
	mediaUrl := getCorrectUrl(media)

	// Get extension from URL
	ext := path.Ext(mediaUrl)
	if ext == "" {
		if media.Is_video {
			ext = ".mp4"
		} else {
			ext = ".jpg"
		}
	}

	// Use the _id as filename
	return fmt.Sprintf("%s%s", media.ID, ext), nil
}

// displayCompletedMessages shows completed messages above the progress bar
func (s *Scraper) displayCompletedMessages() {
	for msg := range s.completedChan {
		// Clear the current line and move up
		fmt.Print("\033[K\033[A")
		fmt.Println(msg)
		// Redraw progress bar
		s.progressBar.RenderBlank()
	}
}

func (s *Scraper) SaveMediaToFile(media Media, folderPath string) error {
	mediaUrl := getCorrectUrl(media)
	mediaUrl = fixUrl(mediaUrl)

	mediaFile, err := getMediaFilename(media)
	if err != nil {
		return err
	}

	mediaPath := path.Join(folderPath, mediaFile)

	// Skip if file already exists
	if _, err := os.Stat(mediaPath); err == nil {
		s.completedChan <- fmt.Sprintf("⏭️  Skipped existing: %s", mediaFile)
		s.progressBar.Add(1)
		return nil
	}

	err = s.httpClient.DownloadFile(mediaUrl, mediaPath)
	if err != nil {
		return fmt.Errorf("failed to download media: %w", err)
	}

	// Set modification time
	imageTime := time.Unix(media.Upload_date/1000, 0)
	os.Chtimes(mediaPath, imageTime, imageTime)

	s.completedChan <- fmt.Sprintf("✅ Completed: %s", mediaFile)
	s.progressBar.Add(1)
	return nil
}

func (s *Scraper) SaveProfilePicture() error {
	userPath, err := s.createUserDirectory()
	if err != nil {
		return err
	}

	// Use profile_image_id as filename
	profileFilename := fmt.Sprintf("%s.jpg", s.profileImageID)
	profilePath := path.Join(userPath, profileFilename)
	
	// Skip if already exists
	if _, err := os.Stat(profilePath); err == nil {
		fmt.Printf("⏭️  Profile picture already exists: %s\n", profileFilename)
		return nil
	}

	fmt.Printf("⬇️  Downloading profile picture: %s\n", profileFilename)
	
	// Construct the profile image URL using the ID
	profileURL := fmt.Sprintf("https://i.vsco.co/%s", s.profileImageID)

	err = s.httpClient.DownloadFile(profileURL, profilePath)
	if err != nil {
		return fmt.Errorf("failed to download profile picture: %w", err)
	}

	fmt.Printf("✅ Completed profile picture: %s\n\n", profileFilename)
	return nil
}

func (s *Scraper) stripExistingMedia(mediaList imageList, userPath string) (imageList, error) {
	var strippedList imageList

	for _, media := range mediaList.Media {
		mediaFilename, err := getMediaFilename(media)

		if err != nil {
			return imageList{}, err
		}

		if _, err := os.Stat(path.Join(userPath, mediaFilename)); os.IsNotExist(err) {
			strippedList.Media = append(strippedList.Media, media)
		}
	}

	fmt.Printf("Found %d new media items out of %d total\n", len(strippedList.Media), len(mediaList.Media))
	return strippedList, nil
}

func (s *Scraper) createUserDirectory() (string, error) {
	userPath := path.Join(s.downloadDir, s.username)

	err := os.MkdirAll(userPath, 0755)
	if err != nil {
		return "", fmt.Errorf("could not create directory %s: %w", userPath, err)
	}

	return userPath, nil
}

func (s *Scraper) SaveAllMedia() error {
	fmt.Printf("Fetching media list for %s...\n", s.username)
	
	imagelist, err := s.fetchImageList()
	if err != nil {
		return err
	}

	if len(imagelist.Media) == 0 {
		fmt.Printf("No media found for user: %s\n", s.username)
		return nil
	}

	userPath, err := s.createUserDirectory()
	if err != nil {
		return err
	}

	// Strip our list so we don't save duplicates
	imagelist, err = s.stripExistingMedia(imagelist, userPath)
	if err != nil {
		return err
	}

	if len(imagelist.Media) == 0 {
		fmt.Printf("All media already downloaded for %s\n", s.username)
		return nil
	}

	fmt.Printf("\nDownloading %d media items from %s with %d workers...\n\n", len(imagelist.Media), s.username, s.numWorkers)

	// Create a progress bar for overall progress
	s.progressBar = progressbar.NewOptions(len(imagelist.Media),
		progressbar.OptionSetDescription("Overall Progress"),
		progressbar.OptionSetWriter(os.Stdout),
		progressbar.OptionShowCount(),
		progressbar.OptionShowBytes(false),
		progressbar.OptionSetWidth(30),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "█",
			SaucerHead:    "█",
			SaucerPadding: "░",
			BarStart:      "|",
			BarEnd:        "|",
		}),
		progressbar.OptionOnCompletion(func() {
			fmt.Println()
		}),
	)

	// Start goroutine to display completed messages
	go s.displayCompletedMessages()

	// Concurrent downloads
	var sem = make(chan int, s.numWorkers)
	var wg sync.WaitGroup

	for _, media := range imagelist.Media {
		sem <- 1
		wg.Add(1)
		go func(media Media) {
			defer func() {
				<-sem
				wg.Done()
			}()

			err := s.SaveMediaToFile(media, userPath)
			if err != nil {
				log.Printf("❌ Error downloading media %s: %v", media.ID, err)
			}
		}(media)
	}

	wg.Wait()
	fmt.Printf("\n🎉 Completed all downloads for %s\n", s.username)
	return nil
}

// ========== Main Application ==========
func main() {
	// Parse command line arguments
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Default worker count
	workers := 5

	// Check for flags
	var usernames []string
	var listFile string

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		
		switch arg {
		case "-l":
			if i+1 < len(os.Args) {
				listFile = os.Args[i+1]
				i++
			} else {
				fmt.Println("Error: -l flag requires a filename")
				os.Exit(1)
			}
		case "-w":
			if i+1 < len(os.Args) {
				var err error
				workers, err = strconv.Atoi(os.Args[i+1])
				if err != nil || workers < 1 {
					fmt.Println("Error: -w flag requires a positive integer")
					os.Exit(1)
				}
				i++
			} else {
				fmt.Println("Error: -w flag requires a number")
				os.Exit(1)
			}
		default:
			if !strings.HasPrefix(arg, "-") {
				usernames = append(usernames, arg)
			}
		}
	}

	// Handle list file if provided
	if listFile != "" {
		file, err := os.Open(listFile)
		if err != nil {
			fmt.Printf("Error opening list file: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			username := strings.TrimSpace(scanner.Text())
			if username != "" {
				usernames = append(usernames, username)
			}
		}

		if err := scanner.Err(); err != nil {
			fmt.Printf("Error reading list file: %v\n", err)
			os.Exit(1)
		}
	}

	// Validate we have usernames to scrape
	if len(usernames) == 0 {
		fmt.Println("Error: No usernames provided")
		printUsage()
		os.Exit(1)
	}

	// Process each user
	for _, username := range usernames {
		scraper := NewScraper(username, workers)
		
		// Get user info first
		err := scraper.GetUserInfo()
		if err != nil {
			log.Printf("❌ Error getting user info for %s: %v", username, err)
			scraper.Close()
			continue
		}

		// Download profile picture first
		err = scraper.SaveProfilePicture()
		if err != nil {
			log.Printf("❌ Error saving profile picture for %s: %v", username, err)
		}

		// Then download all media
		err = scraper.SaveAllMedia()
		if err != nil {
			log.Printf("❌ Error saving media for %s: %v", username, err)
		}
		
		scraper.Close()
	}
}

func printUsage() {
	fmt.Println("VSCO Image Scraper")
	fmt.Println("====================")
	fmt.Println("Usage:")
	fmt.Println("  Single user: go run testzz.go username")
	fmt.Println("  Multiple users: go run testzz.go -l usernames.txt")
	fmt.Println("  With workers: go run testzz.go -w 10 username")
	fmt.Println("\nOptions:")
	fmt.Println("  -l FILE    Scrape multiple usernames from a file (one per line)")
	fmt.Println("  -w N       Number of concurrent workers (default: 5)")
	fmt.Println("\nFeatures:")
	fmt.Println("  ✅ Auto-downloads profile picture")
	fmt.Println("  ✅ Shows full filenames during download")
	fmt.Println("  ✅ Overall progress bar always at bottom")
	fmt.Println("  ✅ Concurrent downloads")
	fmt.Println("  ✅ Skips existing files")
	fmt.Println("  ✅ Automatic retry on rate limit (100 attempts, 10s delay)")
	fmt.Println("  ✅ 3-5 second delay between page requests")
}

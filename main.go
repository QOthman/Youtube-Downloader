package main

import (
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/sessions"
	"github.com/kkdai/youtube/v2"
)

var (
	homeTemplate     *template.Template
	downloadTemplate *template.Template
	client           *youtube.Client
	initOnce         sync.Once
	store            = sessions.NewCookieStore([]byte("something-very-secret"))
	dataStore        sync.Map // In-memory store
)

// VideoData holds video information
type VideoData struct {
	Image        string
	Title        string
	QualityVideo []string
	QualityAudio []string
	FormatMap    map[string]*youtube.Format
	Video        *youtube.Video // Added Video field
}

// generateRandomColor generates a random color in hexadecimal format
func generateRandomColor() string {
	rand.Seed(time.Now().UnixNano())
	r := rand.Intn(256)
	g := rand.Intn(256)
	b := rand.Intn(256)
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

// initializeTemplates initializes the templates once
func initializeTemplates() {
	initOnce.Do(func() {
		var err error
		homeTemplate, err = template.ParseFiles("static/home.html")
		if err != nil {
			panic(fmt.Sprintf("Failed to parse home template: %v", err))
		}
		downloadTemplate, err = template.ParseFiles("static/download.html")
		if err != nil {
			panic(fmt.Sprintf("Failed to parse download template: %v", err))
		}
	})
}

// homeHandler handles the home page request
func homeHandler(w http.ResponseWriter, r *http.Request) {
	initializeTemplates()
	color1 := generateRandomColor()
	color2 := generateRandomColor()
	templateData := struct {
		Color1 string
		Color2 string
	}{
		Color1: color1,
		Color2: color2,
	}

	err := homeTemplate.Execute(w, templateData)
	if err != nil {
		http.Error(w, "Failed to render template: "+err.Error(), http.StatusInternalServerError)
	}
}

// searchHandler handles the video search request
func searchHandler(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session-name")

	url := r.FormValue("url")
	if url == "" {
		http.Error(w, "URL parameter is required", http.StatusBadRequest)
		return
	}

	video, err := client.GetVideo(url)
	if err != nil {
		http.Error(w, "Failed to fetch video: "+err.Error(), http.StatusInternalServerError)
		return
	}

	videoData := VideoData{
		Image:     video.Thumbnails[0].URL,
		Title:     video.Title,
		FormatMap: make(map[string]*youtube.Format),
		Video:     video, // Populate Video field
	}

	for _, format := range video.Formats {
		var description string
		if format.AudioChannels > 0 {
			sizeMB := float64(format.ContentLength) / 1048576.0
			if strings.Contains(format.MimeType, "video") {
				description = fmt.Sprintf("%s (%.2fM)", format.QualityLabel, sizeMB)
				videoData.QualityVideo = append(videoData.QualityVideo, description)
			} else if strings.Contains(format.MimeType, "audio") {
				description = fmt.Sprintf("%dkbps (%.2fM)", format.AverageBitrate/1000, sizeMB)
				videoData.QualityAudio = append(videoData.QualityAudio, description)
			}
			videoData.FormatMap[description] = &format
		}
	}

	// Generate a unique ID for this session's video data
	id := uuid.New().String()
	dataStore.Store(id, videoData)

	session.Values["videoDataID"] = id
	if err := session.Save(r, w); err != nil {
		log.Printf("Failed to save session: %v", err)
		http.Error(w, "Failed to save session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := downloadTemplate.Execute(w, videoData); err != nil {
		log.Printf("Failed to render template: %v", err)
		http.Error(w, "Failed to render template: "+err.Error(), http.StatusInternalServerError)
	}
}

// downloadHandler handles the video download request
func downloadHandler(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session-name")

	val, ok := session.Values["videoDataID"].(string)
	if !ok {
		log.Println("Invalid session data ID")
		http.Error(w, "Invalid session data", http.StatusBadRequest)
		return
	}

	// Retrieve the video data from the in-memory store
	v, ok := dataStore.Load(val)
	if !ok {
		log.Println("Video data not found in store")
		http.Error(w, "Video data not found", http.StatusBadRequest)
		return
	}
	videoData, ok := v.(VideoData)
	if !ok {
		log.Println("Failed to assert video data")
		http.Error(w, "Invalid session data", http.StatusBadRequest)
		return
	}

	quality := r.FormValue("Quality")
	format, exists := videoData.FormatMap[quality]
	if !exists {
		http.Error(w, "Unsupported format", http.StatusBadRequest)
		return
	}

	stream, _, err := client.GetStream(videoData.Video, format) // Use videoData.Video
	if err != nil {
		log.Printf("Failed to get video stream: %v", err)
		http.Error(w, "Failed to get video stream: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer stream.Close()

	var fileExtension, contentType string
	if strings.Contains(quality, "kbps") {
		fileExtension = ".mp3"
		contentType = "audio/mpeg"
	} else if strings.Contains(quality, "p") {
		fileExtension = ".mp4"
		contentType = "video/mp4"
	} else {
		http.Error(w, "Unsupported format", http.StatusBadRequest)
		return
	}

	fileName := "download" + fileExtension
	w.Header().Set("Content-Disposition", "attachment; filename="+fileName)
	w.Header().Set("Content-Type", contentType)

	if _, err := io.Copy(w, stream); err != nil {
		log.Printf("Failed to write video stream to response: %v", err)
		http.Error(w, "Failed to write video stream to response: "+err.Error(), http.StatusInternalServerError)
	}
}

// main starts the server and handles the routes
func main() {
	client = &youtube.Client{}

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/search", searchHandler)
	http.HandleFunc("/download", downloadHandler)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	fmt.Println("Starting server at :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Println("Failed to start server:", err)
	}
}

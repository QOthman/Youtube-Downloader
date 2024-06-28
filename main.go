package main

import (
	"fmt"
	"html/template"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"

	"github.com/kkdai/youtube/v2"
)

// VideoData holds video information
type VideoData struct {
	Image        string
	Title        string
	QualityVideo []string
	QualityAudio []string
	client       *youtube.Client
	video        *youtube.Video
	formatMap    map[string]*youtube.Format
}

// TemplateData holds color information for the template
type TemplateData struct {
	Color1 string
	Color2 string
}

var (
	templateData     TemplateData
	videoData        VideoData
	homeTemplate     *template.Template
	downloadTemplate *template.Template
	client           *youtube.Client
	initOnce         sync.Once
)

// generateRandomColor generates a random color in hexadecimal format
func generateRandomColor() string {
	r := rand.Intn(256)
	g := rand.Intn(256)
	b := rand.Intn(256)
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

// initializeTemplates initializes the templates once
func initializeTemplates() {
	initOnce.Do(func() {
		homeTemplate = template.Must(template.ParseFiles("static/home.html"))
		downloadTemplate = template.Must(template.ParseFiles("static/download.html"))
	})
}

// homeHandler handles the home page request
func homeHandler(w http.ResponseWriter, r *http.Request) {
	initializeTemplates()
	color1 := generateRandomColor()
	color2 := generateRandomColor()
	templateData.Color1 = color1
	templateData.Color2 = color2

	homeTemplate.Execute(w, templateData)
}

// searchHandler handles the video search request
func searchHandler(w http.ResponseWriter, r *http.Request) {
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

	videoData = VideoData{
		video:     video,
		client:    client,
		formatMap: make(map[string]*youtube.Format),
	}

	for _, format := range video.Formats {
		var description string
		if format.AudioChannels > 0 {
			if strings.Contains(format.MimeType, "video") {
				if format.ContentLength == 0 {
					sizeMB := float64((float64(format.Bitrate/8) * video.Duration.Seconds()) / 1048576.0)
					description = fmt.Sprintf("%s (%.2fM)", format.QualityLabel, sizeMB)
				} else {
					sizeMB := float64(format.ContentLength) / 1048576.0
					description = fmt.Sprintf("%s (%.2fM)", format.QualityLabel, sizeMB)
				}
			} else if strings.Contains(format.MimeType, "audio") {
				if format.ContentLength == 0 {
					sizeMB := float64((float64(format.Bitrate/8) * video.Duration.Seconds()) / 1048576.0)
					description = fmt.Sprintf("%dkbps (%.2fM)", format.AverageBitrate/1000, sizeMB)
				} else {
					sizeMB := float64(format.ContentLength) / 1048576.0
					description = fmt.Sprintf("%dkbps (%.2fM)", format.AverageBitrate/1000, sizeMB)
				}
			}
			if strings.Contains(format.MimeType, "video") {
				videoData.QualityVideo = append(videoData.QualityVideo, description)
			} else if strings.Contains(format.MimeType, "audio") {
				videoData.QualityAudio = append(videoData.QualityAudio, description)
			}
			videoData.formatMap[description] = &format
		}

	}

	videoData.Image = video.Thumbnails[0].URL
	videoData.Title = video.Title

	downloadTemplate.Execute(w, videoData)
}

// downloadHandler handles the video download request
func downloadHandler(w http.ResponseWriter, r *http.Request) {
	quality := r.FormValue("Quality")
	format, exists := videoData.formatMap[quality]
	if !exists {
		http.Error(w, "Unsupported format", http.StatusBadRequest)
		return
	}

	stream, _, err := client.GetStream(videoData.video, format)
	if err != nil {
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
		http.Error(w, "Failed to write video stream to response: "+err.Error(), http.StatusInternalServerError)
		return
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

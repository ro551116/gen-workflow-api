package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// --- API request/response types ---

type BackdropRequest struct {
	ImageURL    string       `json:"image_url"`
	MaskRegions []MaskRegion `json:"mask_regions,omitempty"`
	Duration    string       `json:"duration,omitempty"`
	Loop        bool         `json:"loop,omitempty"`
	Crossfade   string       `json:"crossfade,omitempty"`
	Speed       float64      `json:"speed,omitempty"`
	Prompt      string       `json:"prompt,omitempty"`
}

type TaskResponse struct {
	TaskID   string `json:"task_id"`
	Status   string `json:"status"`
	VideoURL string `json:"video_url,omitempty"`
	Error    string `json:"error,omitempty"`
}

// --- In-memory task store ---

type taskEntry struct {
	mu       sync.RWMutex
	ID       string
	Status   string
	VideoURL string
	Error    string
}

var (
	taskStore   = make(map[string]*taskEntry)
	taskStoreMu sync.RWMutex
)

func newTaskID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func storeTask(t *taskEntry) {
	taskStoreMu.Lock()
	taskStore[t.ID] = t
	taskStoreMu.Unlock()
}

func loadTask(id string) *taskEntry {
	taskStoreMu.RLock()
	defer taskStoreMu.RUnlock()
	return taskStore[id]
}

// --- HTTP handlers ---

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": "0.3.0"})
}

func handleBackdrop(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 50 MB (base64 images can be large)
	r.Body = http.MaxBytesReader(w, r.Body, 50*1024*1024)

	var req BackdropRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.ImageURL == "" {
		httpError(w, http.StatusBadRequest, "image_url is required")
		return
	}

	// Defaults
	if req.Duration == "" {
		req.Duration = "5"
	}
	if req.Crossfade == "" {
		req.Crossfade = "0.5"
	}

	taskID := newTaskID()
	entry := &taskEntry{ID: taskID, Status: "processing"}
	storeTask(entry)

	baseURL := resolveBaseURL(r)
	go processBackdrop(entry, req, baseURL)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(TaskResponse{
		TaskID: taskID,
		Status: "processing",
	})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t := loadTask(id)
	if t == nil {
		httpError(w, http.StatusNotFound, "task not found")
		return
	}

	t.mu.RLock()
	resp := TaskResponse{
		TaskID:   t.ID,
		Status:   t.Status,
		VideoURL: t.VideoURL,
		Error:    t.Error,
	}
	t.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	filename := filepath.Base(r.PathValue("filename"))
	// Only serve files with our prefix
	if !strings.HasPrefix(filename, "fal-") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	path := filepath.Join(os.TempDir(), filename)
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	http.ServeFile(w, r, path)
}

// --- Background processing ---

func processBackdrop(entry *taskEntry, req BackdropRequest, baseURL string) {
	apiKey := os.Getenv("FAL_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("FAL_API_KEY")
	}
	if apiKey == "" {
		taskFail(entry, "FAL_KEY environment variable not set")
		return
	}

	client := NewClient(apiKey)

	// Default prompt
	prompt := req.Prompt
	if prompt == "" {
		prompt = "subtle gentle motion, golden particles floating slowly, geometric shapes gently rotating, gradient light flowing softly, decorative elements shimmering with soft light, text and logo remain perfectly still and sharp, elegant corporate event backdrop, smooth loop animation"
	}

	// Build fal.ai request
	falReq := V21Request{
		Prompt:         prompt,
		ImageURL:       req.ImageURL,
		Duration:       req.Duration,
		AspectRatio:    "16:9",
		NegativePrompt: "blur, distort, low quality, text deformation, logo distortion, warping letters",
		CfgScale:       0.5,
	}

	// Generate mask from regions if provided
	if len(req.MaskRegions) > 0 {
		w, h, err := getImageDimensions(req.ImageURL)
		if err != nil {
			taskFail(entry, fmt.Sprintf("get image dimensions: %v", err))
			return
		}
		log.Printf("[%s] image: %dx%d, generating mask with %d regions", entry.ID, w, h, len(req.MaskRegions))

		maskURI, err := generateMaskDataURI(w, h, req.MaskRegions)
		if err != nil {
			taskFail(entry, fmt.Sprintf("generate mask: %v", err))
			return
		}
		falReq.StaticMaskURL = maskURI
	}

	// Submit to fal.ai
	log.Printf("[%s] submitting to Kling v2.1 Pro (duration=%s)", entry.ID, req.Duration)
	qr, err := client.Submit(endpointKlingV21Pro, falReq)
	if err != nil {
		taskFail(entry, fmt.Sprintf("submit: %v", err))
		return
	}
	log.Printf("[%s] fal.ai request_id: %s", entry.ID, qr.RequestID)

	// Poll until complete
	result, err := client.WaitForResult(qr, defaultTimeout)
	if err != nil {
		taskFail(entry, fmt.Sprintf("wait: %v", err))
		return
	}
	if result.Video.URL == "" {
		taskFail(entry, "no video URL in result")
		return
	}
	log.Printf("[%s] fal.ai video ready: %s", entry.ID, result.Video.URL)

	// If no post-processing needed, return fal.ai URL directly
	needsPostProcess := req.Loop || (req.Speed > 0 && req.Speed != 1.0)
	if !needsPostProcess {
		taskComplete(entry, result.Video.URL)
		return
	}

	// Download video for post-processing
	tmpInput := filepath.Join(os.TempDir(), fmt.Sprintf("fal-%s-raw.mp4", entry.ID))
	if err := client.Download(result.Video.URL, tmpInput); err != nil {
		taskFail(entry, fmt.Sprintf("download: %v", err))
		return
	}

	currentPath := tmpInput

	// 1) Speed adjustment + frame interpolation
	if req.Speed > 0 && req.Speed != 1.0 {
		log.Printf("[%s] applying speed=%.2f", entry.ID, req.Speed)
		slowPath, err := applySpeed(currentPath, req.Speed)
		if err != nil {
			taskFail(entry, fmt.Sprintf("speed: %v", err))
			return
		}
		os.Remove(currentPath)
		currentPath = slowPath
	}

	// 2) Seamless loop via xfade
	if req.Loop {
		log.Printf("[%s] applying loop (crossfade=%s)", entry.ID, req.Crossfade)
		loopPath, err := makeLoop(currentPath, req.Crossfade)
		if err != nil {
			taskFail(entry, fmt.Sprintf("loop: %v", err))
			return
		}
		os.Remove(currentPath)
		currentPath = loopPath
	}

	// Rename to final downloadable path
	finalName := fmt.Sprintf("fal-%s.mp4", entry.ID)
	finalPath := filepath.Join(os.TempDir(), finalName)
	if err := os.Rename(currentPath, finalPath); err != nil {
		taskFail(entry, fmt.Sprintf("rename: %v", err))
		return
	}

	videoURL := fmt.Sprintf("%s/api/download/%s", baseURL, finalName)
	taskComplete(entry, videoURL)
}

// applySpeed slows down video and interpolates frames to 30fps.
func applySpeed(inputPath string, speed float64) (string, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg not found in PATH")
	}

	outputPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + "_slow.mp4"
	factor := 1.0 / speed // speed 0.7 → factor 1.4286 → slower

	// Try minterpolate for smooth slow-motion
	filter := fmt.Sprintf("setpts=%.4f*PTS,minterpolate=fps=30:mi_mode=blend", factor)
	cmd := exec.Command(ffmpeg, "-y", "-i", inputPath,
		"-vf", filter,
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "18", "-an",
		outputPath,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Fallback: simple setpts without interpolation
		log.Printf("[speed] minterpolate unavailable, falling back to simple speed change")
		filter = fmt.Sprintf("setpts=%.4f*PTS", factor)
		cmd = exec.Command(ffmpeg, "-y", "-i", inputPath,
			"-vf", filter, "-r", "30",
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "18", "-an",
			outputPath,
		)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("ffmpeg speed: %w", err)
		}
	}
	return outputPath, nil
}

// --- Task state helpers ---

func taskFail(entry *taskEntry, msg string) {
	entry.mu.Lock()
	entry.Status = "failed"
	entry.Error = msg
	entry.mu.Unlock()
	log.Printf("[%s] FAILED: %s", entry.ID, msg)
}

func taskComplete(entry *taskEntry, videoURL string) {
	entry.mu.Lock()
	entry.Status = "completed"
	entry.VideoURL = videoURL
	entry.mu.Unlock()
	log.Printf("[%s] COMPLETED: %s", entry.ID, videoURL)
}

// --- Helpers ---

func resolveBaseURL(r *http.Request) string {
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// --- Server entry point ---

func cmdServe(args []string) error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleIndex)
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /api/backdrop", handleBackdrop)
	mux.HandleFunc("GET /api/status/{id}", handleStatus)
	mux.HandleFunc("GET /api/download/{filename}", handleDownload)

	log.Printf("fal-cli server v0.3.0 listening on :%s", port)
	return http.ListenAndServe(":"+port, mux)
}

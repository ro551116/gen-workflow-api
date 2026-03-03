package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	queueBase = "https://queue.fal.run"
	// Kling v2.1 Pro — supports static_mask + dynamic_masks
	endpointKlingV21Pro = "fal-ai/kling-video/v2.1/pro/image-to-video"
	// Kling v2.6 Pro — native audio, no brush support
	endpointKlingV26Pro = "fal-ai/kling-video/v2.6/pro/image-to-video"
	// Kling v3 Pro — latest, elements support
	endpointKlingV3Pro = "fal-ai/kling-video/v3/pro/image-to-video"

	defaultPollInterval = 10 * time.Second
	defaultTimeout      = 15 * time.Minute
)

// --- Request/Response types ---

type QueueResponse struct {
	RequestID   string `json:"request_id"`
	ResponseURL string `json:"response_url"`
	StatusURL   string `json:"status_url"`
	CancelURL   string `json:"cancel_url"`
}

type StatusResponse struct {
	Status        string `json:"status"`
	QueuePosition int    `json:"queue_position,omitempty"`
}

type VideoFile struct {
	URL         string `json:"url"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	FileSize    int64  `json:"file_size"`
}

type VideoOutput struct {
	Video VideoFile `json:"video"`
}

type ResultResponse struct {
	Video VideoFile `json:"video"`
}

type DynamicMask struct {
	MaskURL      string       `json:"mask_url"`
	Trajectories []Trajectory `json:"trajectories"`
}

type Trajectory struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// v2.1 request (supports brush)
type V21Request struct {
	Prompt         string        `json:"prompt"`
	ImageURL       string        `json:"image_url"`
	Duration       string        `json:"duration,omitempty"`
	AspectRatio    string        `json:"aspect_ratio,omitempty"`
	NegativePrompt string        `json:"negative_prompt,omitempty"`
	CfgScale       float64       `json:"cfg_scale,omitempty"`
	StaticMaskURL  string        `json:"static_mask_url,omitempty"`
	DynamicMasks   []DynamicMask `json:"dynamic_masks,omitempty"`
}

// v2.6 request (native audio)
type V26Request struct {
	Prompt         string `json:"prompt"`
	StartImageURL  string `json:"start_image_url"`
	Duration       string `json:"duration,omitempty"`
	NegativePrompt string `json:"negative_prompt,omitempty"`
	GenerateAudio  *bool  `json:"generate_audio,omitempty"`
}

// --- API Client ---

type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) doRequest(method, rawURL string, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Key "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

func (c *Client) Submit(endpoint string, input interface{}) (*QueueResponse, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	u := fmt.Sprintf("%s/%s", queueBase, endpoint)
	data, code, err := c.doRequest("POST", u, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("submit: %w", err)
	}
	if code != 200 && code != 202 {
		return nil, fmt.Errorf("submit failed (HTTP %d): %s", code, string(data))
	}
	var qr QueueResponse
	if err := json.Unmarshal(data, &qr); err != nil {
		return nil, fmt.Errorf("parse queue response: %w", err)
	}
	return &qr, nil
}

func (c *Client) PollStatus(statusURL string) (*StatusResponse, error) {
	data, _, err := c.doRequest("GET", statusURL, nil)
	if err != nil {
		return nil, err
	}
	var sr StatusResponse
	if err := json.Unmarshal(data, &sr); err != nil {
		return nil, fmt.Errorf("poll status: %w\nraw: %s", err, string(data))
	}
	return &sr, nil
}

func (c *Client) GetResult(responseURL string) (*ResultResponse, error) {
	data, code, err := c.doRequest("GET", responseURL, nil)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("get result (HTTP %d): %s", code, string(data))
	}
	var rr ResultResponse
	if err := json.Unmarshal(data, &rr); err != nil {
		return nil, fmt.Errorf("parse result: %w\nraw: %s", err, string(data))
	}
	return &rr, nil
}

func (c *Client) WaitForResult(qr *QueueResponse, timeout time.Duration) (*ResultResponse, error) {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout after %v", timeout)
		}
		st, err := c.PollStatus(qr.StatusURL)
		if err != nil {
			return nil, err
		}
		switch st.Status {
		case "COMPLETED":
			return c.GetResult(qr.ResponseURL)
		case "IN_QUEUE":
			fmt.Fprintf(os.Stderr, "⏳ 排隊中（位置 %d）...\n", st.QueuePosition)
		case "IN_PROGRESS":
			fmt.Fprintf(os.Stderr, "🔄 生成中...\n")
		default:
			return nil, fmt.Errorf("unexpected status: %s", st.Status)
		}
		time.Sleep(defaultPollInterval)
	}
}

func (c *Client) Download(videoURL, outputPath string) error {
	resp, err := http.Get(videoURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✅ 下載完成：%s（%.1f MB）\n", outputPath, float64(n)/1024/1024)
	return nil
}

// --- File helpers ---

func fileToDataURI(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(path))
	mime := "image/png"
	switch ext {
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".webp":
		mime = "image/webp"
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mime, b64), nil
}

func isURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

func resolveImageInput(input string) (string, error) {
	if isURL(input) {
		return input, nil
	}
	// local file → base64 data URI
	if _, err := os.Stat(input); err != nil {
		return "", fmt.Errorf("file not found: %s", input)
	}
	fmt.Fprintf(os.Stderr, "📎 編碼圖片：%s\n", input)
	return fileToDataURI(input)
}

// --- ffmpeg loop ---

func makeLoop(inputPath, crossfade string) (string, error) {
	// Check ffmpeg exists
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg not found in PATH — install with: brew install ffmpeg")
	}

	// Probe duration
	dur, err := probeDuration(inputPath)
	if err != nil {
		return "", fmt.Errorf("probe duration: %w", err)
	}

	trim := fmt.Sprintf("%.6f", dur-parseCrossfade(crossfade))
	loopPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + "_loop.mp4"

	filter := fmt.Sprintf(
		"[0]split[a][b]; [a]trim=0:%s,setpts=PTS-STARTPTS[main]; [b]trim=%s:%f,setpts=PTS-STARTPTS[tail]; [tail][main]xfade=transition=fade:duration=%s:offset=0[out]",
		trim, trim, dur, crossfade,
	)

	cmd := exec.Command(ffmpeg, "-y", "-i", inputPath,
		"-filter_complex", filter,
		"-map", "[out]",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "18",
		loopPath,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg: %w", err)
	}
	return loopPath, nil
}

func probeDuration(path string) (float64, error) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0, fmt.Errorf("ffprobe not found")
	}
	out, err := exec.Command(ffprobe,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		path,
	).Output()
	if err != nil {
		return 0, err
	}
	var dur float64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &dur); err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", string(out), err)
	}
	return dur, nil
}

func parseCrossfade(s string) float64 {
	var v float64
	fmt.Sscanf(s, "%f", &v)
	if v <= 0 {
		return 0.5
	}
	return v
}

// --- Commands ---

func cmdBackdrop(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: fal-cli backdrop <image> [--mask <mask>] [--prompt \"...\"] [--duration 5|10] [--output <file>]")
	}

	imageInput := args[0]
	var maskInput, prompt, outputPath, duration, crossfade string
	var loop bool
	duration = "5"
	crossfade = "0.5"
	prompt = "subtle gentle motion, golden particles floating slowly, geometric shapes gently rotating, gradient light flowing softly, decorative elements shimmering with soft light, text and logo remain perfectly still and sharp, elegant corporate event backdrop, smooth loop animation"

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--mask", "-m":
			i++
			if i < len(args) {
				maskInput = args[i]
			}
		case "--prompt", "-p":
			i++
			if i < len(args) {
				prompt = args[i]
			}
		case "--duration", "-d":
			i++
			if i < len(args) {
				duration = args[i]
			}
		case "--output", "-o":
			i++
			if i < len(args) {
				outputPath = args[i]
			}
		case "--loop", "-l":
			loop = true
		case "--crossfade", "-cf":
			i++
			if i < len(args) {
				crossfade = args[i]
			}
		}
	}

	apiKey := os.Getenv("FAL_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("FAL_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("FAL_KEY 環境變數未設定")
	}

	client := NewClient(apiKey)

	// Resolve image
	imageURL, err := resolveImageInput(imageInput)
	if err != nil {
		return err
	}

	// Build request
	req := V21Request{
		Prompt:         prompt,
		ImageURL:       imageURL,
		Duration:       duration,
		AspectRatio:    "16:9",
		NegativePrompt: "blur, distort, low quality, text deformation, logo distortion, warping letters",
		CfgScale:       0.5,
	}

	// Resolve mask if provided
	if maskInput != "" {
		maskURL, err := resolveImageInput(maskInput)
		if err != nil {
			return fmt.Errorf("mask: %w", err)
		}
		req.StaticMaskURL = maskURL
		fmt.Fprintf(os.Stderr, "🎭 使用靜態遮罩：白色區域將保持不動\n")
	}

	// Submit
	fmt.Fprintf(os.Stderr, "🚀 提交 Kling v2.1 Pro 動態背板任務...\n")
	fmt.Fprintf(os.Stderr, "   Prompt: %s\n", truncate(prompt, 80))
	fmt.Fprintf(os.Stderr, "   Duration: %ss\n", duration)

	qr, err := client.Submit(endpointKlingV21Pro, req)
	if err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	fmt.Fprintf(os.Stderr, "📋 Request ID: %s\n", qr.RequestID)

	// Poll
	result, err := client.WaitForResult(qr, defaultTimeout)
	if err != nil {
		return err
	}

	// Output
	if result.Video.URL == "" {
		return fmt.Errorf("no video URL in result")
	}

	fmt.Fprintf(os.Stderr, "🎬 影片 URL: %s\n", result.Video.URL)

	// Download
	if outputPath == "" {
		outputPath = fmt.Sprintf("backdrop_%s.mp4", time.Now().Format("20060102_150405"))
	}
	if err := client.Download(result.Video.URL, outputPath); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Loop
	if loop {
		loopPath, err := makeLoop(outputPath, crossfade)
		if err != nil {
			return fmt.Errorf("loop: %w", err)
		}
		// Replace original with looped version
		if err := os.Remove(outputPath); err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		if err := os.Rename(loopPath, outputPath); err != nil {
			return fmt.Errorf("rename: %w", err)
		}
		fmt.Fprintf(os.Stderr, "🔁 無縫循環完成：%s\n", outputPath)
	}

	// Print URL to stdout for piping
	fmt.Println(result.Video.URL)
	return nil
}

func cmdStatus(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: fal-cli status <request-id>")
	}
	requestID := args[0]

	apiKey := os.Getenv("FAL_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("FAL_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("FAL_KEY 環境變數未設定")
	}

	// Use the short path format that fal.ai returns
	statusURL := fmt.Sprintf("%s/fal-ai/kling-video/requests/%s/status", queueBase, requestID)
	client := NewClient(apiKey)
	st, err := client.PollStatus(statusURL)
	if err != nil {
		return err
	}

	out, _ := json.MarshalIndent(st, "", "  ")
	fmt.Println(string(out))
	return nil
}

func cmdResult(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: fal-cli result <request-id> [--output <file>]")
	}
	requestID := args[0]
	var outputPath string

	for i := 1; i < len(args); i++ {
		if (args[i] == "--output" || args[i] == "-o") && i+1 < len(args) {
			outputPath = args[i+1]
			i++
		}
	}

	apiKey := os.Getenv("FAL_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("FAL_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("FAL_KEY 環境變數未設定")
	}

	responseURL := fmt.Sprintf("%s/fal-ai/kling-video/requests/%s", queueBase, requestID)
	client := NewClient(apiKey)
	result, err := client.GetResult(responseURL)
	if err != nil {
		return err
	}

	if result.Video.URL != "" {
		fmt.Println(result.Video.URL)
		if outputPath != "" {
			return client.Download(result.Video.URL, outputPath)
		}
	} else {
		out, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(out))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func usage() {
	fmt.Fprintf(os.Stderr, `fal-cli — fal.ai Kling Image-to-Video CLI & API Server

Commands:
  backdrop <image> [flags]   靜態背板 → 動態影片（Kling v2.1 Pro + Static Brush）
  status <request-id>        查詢任務狀態
  result <request-id>        取得任務結果
  serve                      啟動 HTTP API 伺服器

Backdrop flags:
  --mask, -m <file>          靜態遮罩（白色=不動，黑色=可動）
  --prompt, -p <text>        動態效果描述（有預設值）
  --duration, -d <5|10>      影片長度秒數（預設 5）
  --output, -o <file>        輸出檔案路徑（預設自動命名）
  --loop, -l                 ffmpeg xfade 無縫循環（需安裝 ffmpeg）
  --crossfade, -cf <sec>     循環交叉溶解秒數（預設 0.5）

Server:
  PORT                       監聽埠號（預設 8080）
  API: POST /api/backdrop    提交動態背板生成任務
  API: GET  /api/status/:id  查詢任務狀態
  API: GET  /health          健康檢查

Environment:
  FAL_KEY                    fal.ai API Key（必填）

Examples:
  fal-cli backdrop ./背板.png
  fal-cli backdrop ./背板.png --mask ./mask.png --duration 10 --loop
  fal-cli backdrop ./背板.png -m ./mask.png -d 10 -l -cf 0.8
  fal-cli serve
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "backdrop":
		err = cmdBackdrop(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "result":
		err = cmdResult(os.Args[2:])
	case "help", "--help", "-h":
		usage()
	case "serve":
		err = cmdServe(os.Args[2:])
	case "version", "--version":
		fmt.Println("fal-cli v0.3.0")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
}

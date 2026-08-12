// Package minimax provides MiniMax media model integrations.
package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-kratos/blades"
)

const (
	// ModelH3 is the video generation model supported by the v2 API.
	ModelH3 = "MiniMax-H3"
	// DefaultVideoModel is the recommended video generation model.
	DefaultVideoModel = ModelH3
	// VideoResolution2K is the supported output resolution.
	VideoResolution2K = "2K"

	globalVideoBaseURL = "https://api.minimax.io"
	chinaVideoBaseURL  = "https://api.minimaxi.com"
	defaultDuration    = 5
	defaultRatio       = "16:9"
	defaultPollCount   = 300
	maxResponseBytes   = 2 << 20
)

var (
	ErrVideoRequestNil      = errors.New("minimax/video: request is nil")
	ErrVideoModelRequired   = errors.New("minimax/video: model is required")
	ErrVideoAPIKeyRequired  = errors.New("minimax/video: API key is required")
	ErrVideoPromptRequired  = errors.New("minimax/video: prompt is required")
	ErrVideoPromptTooLong   = errors.New("minimax/video: prompt exceeds 7000 characters")
	ErrVideoDurationInvalid = errors.New("minimax/video: duration must be an integer from 4 to 15 seconds")
	ErrVideoRatioInvalid    = errors.New("minimax/video: ratio is invalid for text-to-video")
	ErrVideoTaskIncomplete  = errors.New("minimax/video: task did not finish before the polling limit")
)

// VideoRegion selects a regional v2 video API host.
type VideoRegion string

const (
	VideoRegionGlobal VideoRegion = "global_en"
	VideoRegionChina  VideoRegion = "cn_zh"
)

// VideoConfig configures v2 text-to-video generation.
type VideoConfig struct {
	Region        VideoRegion
	BaseURL       string
	APIKey        string
	Duration      int
	Ratio         string
	CallbackURL   string
	AIGCWatermark *bool
	PollInterval  time.Duration
	MaxPollCount  int
	HTTPClient    *http.Client
}

// CreateVideoRequest is the text-to-video task payload accepted by CreateVideo.
type CreateVideoRequest struct {
	Prompt        string
	Duration      int
	Ratio         string
	CallbackURL   string
	AIGCWatermark *bool
}

type createVideoPayload struct {
	Model         string         `json:"model"`
	Content       []videoContent `json:"content"`
	Resolution    string         `json:"resolution"`
	Duration      int            `json:"duration"`
	Ratio         string         `json:"ratio"`
	CallbackURL   string         `json:"callback_url,omitempty"`
	AIGCWatermark *bool          `json:"aigc_watermark,omitempty"`
}

type videoContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CreateVideoResponse identifies an asynchronous generation task.
type CreateVideoResponse struct {
	TaskID string `json:"task_id"`
}

// VideoTaskError describes a failed asynchronous task.
type VideoTaskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// VideoTaskContent contains a completed task's time-limited video URL.
type VideoTaskContent struct {
	URL string `json:"url"`
}

// VideoTaskUsage records the billed media quantities returned by the service.
type VideoTaskUsage struct {
	TotalSeconds  int `json:"total_seconds"`
	InputSeconds  int `json:"input_seconds"`
	OutputSeconds int `json:"output_seconds"`
	ImageCount    int `json:"image_count"`
}

// VideoTask is a v2 video generation task.
type VideoTask struct {
	ID         string           `json:"id"`
	Model      string           `json:"model"`
	Status     string           `json:"status"`
	Error      *VideoTaskError  `json:"error,omitempty"`
	Content    VideoTaskContent `json:"content"`
	Resolution string           `json:"resolution"`
	Duration   int              `json:"duration"`
	Usage      VideoTaskUsage   `json:"usage"`
	Ratio      string           `json:"ratio"`
	TaskType   string           `json:"task_type"`
	Modality   string           `json:"modality"`
}

// ListVideoRequest selects a page of recent generation tasks.
type ListVideoRequest struct {
	PageNumber int
	PageSize   int
	Status     string
	TaskIDs    []string
	Model      string
	TaskType   string
}

// ListVideoResponse is a page of generation tasks.
type ListVideoResponse struct {
	Items []VideoTask `json:"items"`
	Total int         `json:"total"`
}

// DeleteVideoResponse reports whether a task was cancelled or deleted.
type DeleteVideoResponse struct {
	TaskID string `json:"task_id"`
	Action string `json:"action"`
	Status string `json:"status"`
}

// VideoModel implements blades.ModelProvider and exposes task lifecycle methods.
type VideoModel struct {
	model  string
	config VideoConfig
	client *http.Client
}

// NewVideo creates a v2 text-to-video provider.
func NewVideo(model string, config VideoConfig) *VideoModel {
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &VideoModel{model: model, config: config, client: client}
}

// Name returns the configured model name.
func (m *VideoModel) Name() string { return m.model }

// Generate creates a task, polls it, and returns the completed video URL.
func (m *VideoModel) Generate(ctx context.Context, req *blades.ModelRequest) (*blades.ModelResponse, error) {
	if req == nil {
		return nil, ErrVideoRequestNil
	}
	created, err := m.CreateVideo(ctx, CreateVideoRequest{
		Prompt:        promptFromMessages(req.Messages),
		Duration:      m.config.Duration,
		Ratio:         m.config.Ratio,
		CallbackURL:   m.config.CallbackURL,
		AIGCWatermark: m.config.AIGCWatermark,
	})
	if err != nil {
		return nil, err
	}

	pollInterval := m.config.PollInterval
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	pollCount := m.config.MaxPollCount
	if pollCount <= 0 {
		pollCount = defaultPollCount
	}
	for attempt := 0; attempt < pollCount; attempt++ {
		task, queryErr := m.QueryVideo(ctx, created.TaskID)
		if queryErr != nil {
			return nil, queryErr
		}
		switch task.Status {
		case "succeeded":
			if strings.TrimSpace(task.Content.URL) == "" {
				return nil, errors.New("minimax/video: succeeded task is missing content.url")
			}
			message := blades.NewAssistantMessage(blades.StatusCompleted)
			message.Parts = append(message.Parts, blades.FilePart{
				Name:     "video.mp4",
				URI:      task.Content.URL,
				MIMEType: blades.MIMEVideoMP4,
			})
			message.Metadata["task_id"] = task.ID
			message.Metadata["resolution"] = task.Resolution
			message.Metadata["duration"] = task.Duration
			message.Metadata["ratio"] = task.Ratio
			return &blades.ModelResponse{Message: message}, nil
		case "failed", "cancelled":
			if task.Error != nil && task.Error.Message != "" {
				return nil, fmt.Errorf("minimax/video: task %s: %s", task.Status, task.Error.Message)
			}
			return nil, fmt.Errorf("minimax/video: task %s", task.Status)
		}
		if attempt+1 < pollCount {
			timer := time.NewTimer(pollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil, ErrVideoTaskIncomplete
}

// NewStreaming wraps Generate with a single-yield stream.
func (m *VideoModel) NewStreaming(ctx context.Context, req *blades.ModelRequest) blades.Generator[*blades.ModelResponse, error] {
	return func(yield func(*blades.ModelResponse, error) bool) {
		response, err := m.Generate(ctx, req)
		yield(response, err)
	}
}

// CreateVideo submits a v2 text-to-video task.
func (m *VideoModel) CreateVideo(ctx context.Context, request CreateVideoRequest) (*CreateVideoResponse, error) {
	if m.model == "" {
		return nil, ErrVideoModelRequired
	}
	if strings.TrimSpace(m.config.APIKey) == "" {
		return nil, ErrVideoAPIKeyRequired
	}
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return nil, ErrVideoPromptRequired
	}
	if len([]rune(prompt)) > 7000 {
		return nil, ErrVideoPromptTooLong
	}
	duration := request.Duration
	if duration == 0 {
		duration = defaultDuration
	}
	if duration < 4 || duration > 15 {
		return nil, ErrVideoDurationInvalid
	}
	ratio := request.Ratio
	if ratio == "" {
		ratio = defaultRatio
	}
	if !validTextToVideoRatio(ratio) {
		return nil, ErrVideoRatioInvalid
	}
	payload := createVideoPayload{
		Model:       m.model,
		Content:     []videoContent{{Type: "text", Text: prompt}},
		Resolution:  VideoResolution2K,
		Duration:    duration,
		Ratio:       ratio,
		CallbackURL: request.CallbackURL,
	}
	if m.resolveRegion() == VideoRegionChina {
		payload.AIGCWatermark = request.AIGCWatermark
	}
	var response CreateVideoResponse
	if err := m.doJSON(ctx, http.MethodPost, "/v2/video_generation", nil, payload, &response); err != nil {
		return nil, err
	}
	if strings.TrimSpace(response.TaskID) == "" {
		return nil, errors.New("minimax/video: create response is missing task_id")
	}
	return &response, nil
}

// QueryVideo retrieves one task by ID.
func (m *VideoModel) QueryVideo(ctx context.Context, taskID string) (*VideoTask, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, errors.New("minimax/video: task ID is required")
	}
	var response struct {
		Task VideoTask `json:"task"`
	}
	path := "/v2/query/video_generation/" + url.PathEscape(taskID)
	if err := m.doJSON(ctx, http.MethodGet, path, nil, nil, &response); err != nil {
		return nil, err
	}
	return &response.Task, nil
}

// ListVideos lists recent generation tasks.
func (m *VideoModel) ListVideos(ctx context.Context, request ListVideoRequest) (*ListVideoResponse, error) {
	query := make(url.Values)
	if request.PageNumber > 0 {
		query.Set("page_num", strconv.Itoa(request.PageNumber))
	}
	if request.PageSize > 0 {
		query.Set("page_size", strconv.Itoa(request.PageSize))
	}
	if request.Status != "" {
		query.Set("filter.status", request.Status)
	}
	for _, taskID := range request.TaskIDs {
		if taskID = strings.TrimSpace(taskID); taskID != "" {
			query.Add("filter.task_ids", taskID)
		}
	}
	if request.Model != "" {
		query.Set("filter.model", request.Model)
	}
	if request.TaskType != "" {
		query.Set("filter.task_type", request.TaskType)
	}
	var response ListVideoResponse
	if err := m.doJSON(ctx, http.MethodGet, "/v2/query/video_generation", query, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// DeleteVideo cancels a queued task or deletes a completed task record.
func (m *VideoModel) DeleteVideo(ctx context.Context, taskID string) (*DeleteVideoResponse, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, errors.New("minimax/video: task ID is required")
	}
	var response DeleteVideoResponse
	path := "/v2/video_generation/" + url.PathEscape(taskID)
	if err := m.doJSON(ctx, http.MethodDelete, path, nil, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (m *VideoModel) doJSON(ctx context.Context, method, path string, query url.Values, body, result any) error {
	if strings.TrimSpace(m.config.APIKey) == "" {
		return ErrVideoAPIKeyRequired
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("minimax/video: encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	endpoint := strings.TrimRight(m.baseURL(), "/") + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("minimax/video: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.config.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("minimax/video: request: %w", err)
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = decoder.Decode(&apiError)
		if apiError.Error.Message != "" {
			return fmt.Errorf("minimax/video: HTTP %d: %s", response.StatusCode, apiError.Error.Message)
		}
		return fmt.Errorf("minimax/video: HTTP %d", response.StatusCode)
	}
	if result == nil {
		return nil
	}
	if err := decoder.Decode(result); err != nil {
		return fmt.Errorf("minimax/video: decode response: %w", err)
	}
	return nil
}

func (m *VideoModel) resolveRegion() VideoRegion {
	if m.config.Region == VideoRegionChina {
		return VideoRegionChina
	}
	return VideoRegionGlobal
}

func (m *VideoModel) baseURL() string {
	if strings.TrimSpace(m.config.BaseURL) != "" {
		return m.config.BaseURL
	}
	if m.resolveRegion() == VideoRegionChina {
		return chinaVideoBaseURL
	}
	return globalVideoBaseURL
}

func promptFromMessages(messages []*blades.Message) string {
	sections := make([]string, 0, len(messages))
	for _, message := range messages {
		if message != nil && strings.TrimSpace(message.Text()) != "" {
			sections = append(sections, message.Text())
		}
	}
	return strings.Join(sections, "\n")
}

func validTextToVideoRatio(ratio string) bool {
	switch ratio {
	case "adaptive", "21:9", "16:9", "4:3", "1:1", "3:4", "9:16":
		return true
	default:
		return false
	}
}

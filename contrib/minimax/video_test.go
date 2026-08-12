package minimax

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kratos/blades"
)

func TestVideoGenerateCreatesPollsAndReturnsURL(t *testing.T) {
	t.Parallel()

	var pollCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer test-key"; got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
		switch r.URL.Path {
		case "/v2/video_generation":
			if r.Method != http.MethodPost {
				t.Fatalf("create method = %s, want POST", r.Method)
			}
			var body createVideoPayload
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Model != ModelH3 || body.Resolution != VideoResolution2K || body.Duration != 7 || body.Ratio != "9:16" {
				t.Fatalf("create body = %+v", body)
			}
			if len(body.Content) != 1 || body.Content[0].Type != "text" || body.Content[0].Text != "A lighthouse in a storm" {
				t.Fatalf("content = %+v", body.Content)
			}
			writeJSON(t, w, CreateVideoResponse{TaskID: "task-1"})
		case "/v2/query/video_generation/task-1":
			count := pollCount.Add(1)
			status := "running"
			content := VideoTaskContent{}
			if count == 2 {
				status = "succeeded"
				content.URL = "https://cdn.example/video.mp4"
			}
			writeJSON(t, w, map[string]any{"task": VideoTask{
				ID: "task-1", Model: ModelH3, Status: status, Content: content,
				Resolution: VideoResolution2K, Duration: 7, Ratio: "9:16",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	model := NewVideo(ModelH3, VideoConfig{
		BaseURL: server.URL, APIKey: "test-key", Duration: 7, Ratio: "9:16",
		PollInterval: time.Nanosecond, MaxPollCount: 3,
	})
	response, err := model.Generate(context.Background(), &blades.ModelRequest{
		Messages: []*blades.Message{blades.UserMessage("A lighthouse in a storm")},
	})
	if err != nil {
		t.Fatal(err)
	}
	file := response.Message.File()
	if file == nil || file.URI != "https://cdn.example/video.mp4" || file.MIMEType != blades.MIMEVideoMP4 {
		t.Fatalf("file = %+v", file)
	}
	if got := pollCount.Load(); got != 2 {
		t.Fatalf("poll count = %d, want 2", got)
	}
}

func TestVideoLifecycleAndChinaEndpoint(t *testing.T) {
	t.Parallel()

	watermark := true
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/video_generation":
			var body createVideoPayload
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.AIGCWatermark == nil || !*body.AIGCWatermark {
				t.Fatalf("aigc_watermark = %v, want true", body.AIGCWatermark)
			}
			writeJSON(t, w, CreateVideoResponse{TaskID: "task-cn"})
		case r.Method == http.MethodGet && r.URL.Path == "/v2/query/video_generation/task-cn":
			writeJSON(t, w, map[string]any{"task": VideoTask{ID: "task-cn", Status: "queued"}})
		case r.Method == http.MethodGet && r.URL.Path == "/v2/query/video_generation":
			if r.URL.Query().Get("page_num") != "2" || r.URL.Query().Get("filter.status") != "queued" {
				t.Fatalf("list query = %s", r.URL.RawQuery)
			}
			writeJSON(t, w, ListVideoResponse{Items: []VideoTask{{ID: "task-cn", Status: "queued"}}, Total: 1})
		case r.Method == http.MethodDelete && r.URL.Path == "/v2/video_generation/task-cn":
			writeJSON(t, w, DeleteVideoResponse{TaskID: "task-cn", Action: "cancelled", Status: "cancelled"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	model := NewVideo(ModelH3, VideoConfig{Region: VideoRegionChina, BaseURL: server.URL, APIKey: "test", AIGCWatermark: &watermark})
	created, err := model.CreateVideo(context.Background(), CreateVideoRequest{Prompt: "A city at dawn", AIGCWatermark: &watermark})
	if err != nil || created.TaskID != "task-cn" {
		t.Fatalf("CreateVideo() = %+v, %v", created, err)
	}
	queried, err := model.QueryVideo(context.Background(), created.TaskID)
	if err != nil || queried.Status != "queued" {
		t.Fatalf("QueryVideo() = %+v, %v", queried, err)
	}
	listed, err := model.ListVideos(context.Background(), ListVideoRequest{PageNumber: 2, Status: "queued"})
	if err != nil || listed.Total != 1 {
		t.Fatalf("ListVideos() = %+v, %v", listed, err)
	}
	deleted, err := model.DeleteVideo(context.Background(), created.TaskID)
	if err != nil || deleted.Action != "cancelled" {
		t.Fatalf("DeleteVideo() = %+v, %v", deleted, err)
	}
	if got := requests.Load(); got != 4 {
		t.Fatalf("request count = %d, want 4", got)
	}
}

func TestVideoInputValidation(t *testing.T) {
	t.Parallel()

	model := NewVideo(ModelH3, VideoConfig{APIKey: "test"})
	tests := []struct {
		name    string
		request CreateVideoRequest
		want    error
	}{
		{name: "empty prompt", request: CreateVideoRequest{}, want: ErrVideoPromptRequired},
		{name: "duration too short", request: CreateVideoRequest{Prompt: "x", Duration: 3}, want: ErrVideoDurationInvalid},
		{name: "duration too long", request: CreateVideoRequest{Prompt: "x", Duration: 16}, want: ErrVideoDurationInvalid},
		{name: "adaptive text ratio", request: CreateVideoRequest{Prompt: "x", Ratio: "adaptive"}, want: ErrVideoRatioInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := model.CreateVideo(context.Background(), test.request)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestVideoRegionDefaults(t *testing.T) {
	t.Parallel()
	if got := NewVideo(ModelH3, VideoConfig{}).baseURL(); got != globalVideoBaseURL {
		t.Fatalf("global base URL = %q", got)
	}
	if got := NewVideo(ModelH3, VideoConfig{Region: VideoRegionChina}).baseURL(); got != chinaVideoBaseURL {
		t.Fatalf("China base URL = %q", got)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

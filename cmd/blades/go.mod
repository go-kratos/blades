module github.com/go-kratos/blades/cmd/blades

go 1.25.8

require (
	charm.land/bubbles/v2 v2.0.0
	charm.land/bubbletea/v2 v2.0.2
	charm.land/glamour/v2 v2.0.0
	charm.land/lipgloss/v2 v2.0.2
	github.com/go-kratos/blades v0.4.0
	github.com/go-kratos/blades/contrib/anthropic v0.3.0
	github.com/go-kratos/blades/contrib/gemini v0.3.0
	github.com/go-kratos/blades/contrib/mcp v0.1.0
	github.com/go-kratos/blades/contrib/openai v0.3.0
	github.com/google/jsonschema-go v0.3.0
	github.com/google/uuid v1.6.0
	github.com/larksuite/oapi-sdk-go/v3 v3.5.3
	github.com/rivo/uniseg v0.4.7
	github.com/robfig/cron/v3 v3.0.1
	github.com/spf13/cobra v1.8.1
	google.golang.org/genai v1.26.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	cloud.google.com/go v0.116.0 // indirect
	cloud.google.com/go/auth v0.9.3 // indirect
	cloud.google.com/go/compute/metadata v0.8.4 // indirect
	github.com/alecthomas/chroma/v2 v2.20.0 // indirect
	github.com/anthropics/anthropic-sdk-go v1.13.0 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/charmbracelet/colorprofile v0.4.2 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260205113103-524a6607adb8 // indirect
	github.com/charmbracelet/x/ansi v0.11.6 // indirect
	github.com/charmbracelet/x/exp/slice v0.0.0-20250327172914-2fdc97757edf // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/go-kratos/kit v0.0.0-20251121083925-65298ad2aa44 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.6 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-runewidth v0.0.20 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/modelcontextprotocol/go-sdk v1.0.0 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/openai/openai-go/v3 v3.8.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	github.com/yuin/goldmark v1.7.13 // indirect
	github.com/yuin/goldmark-emoji v1.0.6 // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/crypto v0.42.0 // indirect
	golang.org/x/net v0.44.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.30.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250908214217-97024824d090 // indirect
	google.golang.org/grpc v1.75.1 // indirect
	google.golang.org/protobuf v1.36.9 // indirect
)

replace (
	github.com/go-kratos/blades v0.4.0 => ../../
	github.com/go-kratos/blades/contrib/anthropic v0.3.0 => ../../contrib/anthropic
	github.com/go-kratos/blades/contrib/gemini v0.3.0 => ../../contrib/gemini
	github.com/go-kratos/blades/contrib/mcp v0.1.0 => ../../contrib/mcp
	github.com/go-kratos/blades/contrib/openai v0.3.0 => ../../contrib/openai
)

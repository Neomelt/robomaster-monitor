package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// FeishuCard represents the structure of an interactive message card.
type FeishuCard struct {
	MsgType string `json:"msg_type"`
	Card    Card   `json:"card"`
}

type Card struct {
	Config   Config    `json:"config"`
	Header   Header    `json:"header"`
	Elements []Element `json:"elements"`
}

type Config struct {
	WideScreenMode bool `json:"wide_screen_mode"`
	EnableForward  bool `json:"enable_forward"`
}

type Header struct {
	Title    Title  `json:"title"`
	Template string `json:"template"`
}

type Title struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type Element struct {
	Tag     string   `json:"tag"`
	Text    *Text    `json:"text,omitempty"`
	Actions []Action `json:"actions,omitempty"`
}

type Text struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type Action struct {
	Tag  string `json:"tag"`
	Text Text   `json:"text"`
	URL  string `json:"url"`
	Type string `json:"type"`
}

// Send sends a formatted notification to the provided Feishu webhook URL.
func Send(webhookURL, title, articleLink string) error {
	cardMsg := FeishuCard{
		MsgType: "interactive",
		Card: Card{
			Config: Config{
				WideScreenMode: true,
				EnableForward:  true,
			},
			Header: Header{
				Title: Title{
					Tag:     "plain_text",
					Content: "🚀 RoboMaster 论坛有新的开源内容！",
				},
				Template: "blue",
			},
			Elements: []Element{
				{
					Tag: "div",
					Text: &Text{
						Tag:     "lark_md",
						Content: fmt.Sprintf("**%s**", title),
					},
				},
				{
					Tag: "action",
					Actions: []Action{
						{
							Tag: "button",
							Text: Text{
								Tag:     "plain_text",
								Content: "查看详情",
							},
							URL:  articleLink,
							Type: "default",
						},
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(cardMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal feishu card json: %w", err)
	}

	req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send feishu message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("feishu notification failed with status code: %d", resp.StatusCode)
	}

	log.Println("Feishu notification sent successfully.")
	return nil
}

// SendError sends an error notification to the provided Feishu webhook URL.
func SendError(webhookURL, errorMsg string) error {
	cardMsg := FeishuCard{
		MsgType: "interactive",
		Card: Card{
			Config: Config{
				WideScreenMode: true,
				EnableForward:  true,
			},
			Header: Header{
				Title: Title{
					Tag:     "plain_text",
					Content: "🚨 RoboMaster Monitor 运行异常",
				},
				Template: "red",
			},
			Elements: []Element{
				{
					Tag: "div",
					Text: &Text{
						Tag:     "lark_md",
						Content: fmt.Sprintf("**错误详情:**\n%s", errorMsg),
					},
				},
				{
					Tag: "div",
					Text: &Text{
						Tag:     "lark_md",
						Content: "请检查服务器日志以获取更多信息。",
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(cardMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal feishu card json: %w", err)
	}

	req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send feishu message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("feishu notification failed with status code: %d", resp.StatusCode)
	}

	return nil
}

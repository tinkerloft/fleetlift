package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type apiClient struct {
	base  string
	token string
	http  *http.Client
}

func newClient() *apiClient {
	return &apiClient{
		base:  strings.TrimRight(serverURL, "/"),
		token: loadToken(),
		http:  &http.Client{},
	}
}

func (c *apiClient) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *apiClient) post(path string, body any, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequest(http.MethodPost, c.base+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *apiClient) delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.base+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func (c *apiClient) patch(path string, body any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequest(http.MethodPatch, c.base+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, nil)
}

func (c *apiClient) do(req *http.Request, out any) error {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// streamSSE reads SSE events from a GET endpoint and calls onEvent for each data line.
func (c *apiClient) streamSSE(path string, onEvent func(eventType, data string) bool) error {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var eventType string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if !onEvent(eventType, data) {
				return nil
			}
			eventType = ""
		}
	}
	return scanner.Err()
}

func authFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".fleetlift", "auth.json")
}

func loadToken() string {
	data, err := os.ReadFile(authFilePath())
	if err != nil {
		return ""
	}
	var auth struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(data, &auth) != nil {
		return ""
	}
	return auth.Token
}

func saveToken(token string) error {
	dir := filepath.Dir(authFilePath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]string{"token": token})
	return os.WriteFile(authFilePath(), data, 0o600)
}

package loadtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Accounts are the account numbers of a simulated class: LT001, LT002 and so on.
func Accounts(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%03d", prefix, i+1)
	}
	return out
}

// Setup makes what a load test needs on an exam server, through its admin routes: a class of the simulated students and the workspace exam of a
// package that examc compiled (document.json, starter.tar, and grading.tar and image.tar when it has them), published. It returns the exam's ID.
func Setup(ctx context.Context, server, adminToken, packageDir, className, pin string, accounts []string, duration time.Duration) (examID string, err error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	do := func(method, path, contentType string, body io.Reader, want int) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, method, server+path, body)
		if err != nil {
			return nil, err
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		req.Header.Set("Authorization", "Bearer "+adminToken)
		response, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if response.StatusCode != want {
			return data, fmt.Errorf("%s %s = %d: %.200s", method, path, response.StatusCode, strings.TrimSpace(string(data)))
		}
		return data, nil
	}
	var csv strings.Builder
	csv.WriteString("canvas_id,account_number,name,email,sis_user_id\n")
	for i, account := range accounts {
		fmt.Fprintf(&csv, "%d,%s,Load test student %d,%s@example.invalid,s%d\n", i+1, account, i+1, strings.ToLower(account), i+1)
	}
	data, err := do("POST", "/admin/classes?name="+url.QueryEscape(className), "text/csv", strings.NewReader(csv.String()), 201)
	if err != nil {
		return "", fmt.Errorf("making the class: %w", err)
	}
	var class struct {
		Class struct {
			ID string `json:"class_id"`
		} `json:"class"`
	}
	if err := json.Unmarshal(data, &class); err != nil || class.Class.ID == "" {
		return "", fmt.Errorf("the class's answer: %v: %.200s", err, data)
	}
	read := func(name string) ([]byte, error) { return os.ReadFile(filepath.Join(packageDir, name)) }
	raw, err := read("document.json")
	if err != nil {
		return "", err
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return "", err
	}
	document["title"], document["pin"] = className, pin
	document["duration_seconds"], document["grace_seconds"] = int64(duration.Seconds()), int64(120)
	encoded, _ := json.Marshal(document)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	parts := map[string][]byte{"document": encoded}
	for field, file := range map[string]string{"starter": "starter.tar", "grading": "grading.tar", "image": "image.tar"} {
		if content, err := read(file); err == nil {
			parts[field] = content
		} else if field == "starter" {
			return "", err
		}
	}
	for name, content := range parts {
		part, _ := form.CreateFormField(name)
		_, _ = part.Write(content)
	}
	_ = form.Close()
	data, err = do("POST", "/admin/class/"+class.Class.ID+"/workspace-exam", form.FormDataContentType(), &body, 201)
	if err != nil {
		return "", fmt.Errorf("importing the exam: %w", err)
	}
	var created struct {
		ID string `json:"exam_id"`
	}
	if err := json.Unmarshal(data, &created); err != nil || created.ID == "" {
		return "", fmt.Errorf("the exam's answer: %v: %.200s", err, data)
	}
	// Publishing replaces the exam that is published, and the server wants to be told which one that is (so that two proctors do not replace each other's).
	data, err = do("GET", "/admin/exams", "", nil, 200)
	if err != nil {
		return created.ID, fmt.Errorf("reading the exams: %w", err)
	}
	var catalog struct {
		Current string `json:"current_exam_id"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return created.ID, fmt.Errorf("the catalog's answer: %w", err)
	}
	publish, _ := json.Marshal(map[string]any{"expected_current": catalog.Current, "allow_active": true})
	if _, err := do("POST", "/admin/exam/"+created.ID+"/publish", "application/json", bytes.NewReader(publish), 200); err != nil {
		return created.ID, fmt.Errorf("publishing the exam: %w", err)
	}
	return created.ID, nil
}

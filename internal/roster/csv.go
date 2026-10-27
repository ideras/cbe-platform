package roster

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"

	"exam-platform/internal/domain"
)

type Warning struct {
	Row           int
	AccountNumber string
	Message       string
}

type Result struct {
	Students []domain.Student
	Warnings []Warning
}

func Parse(input io.Reader) (Result, error) {
	r := csv.NewReader(input)
	header, err := r.Read()
	if err != nil {
		return Result{}, fmt.Errorf("read roster header: %w", err)
	}
	index := make(map[string]int, len(header))
	for i, name := range header {
		if i == 0 {
			name = strings.TrimPrefix(name, "\ufeff") // spreadsheets add a byte order mark to "CSV UTF-8" exports
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if _, exists := index[name]; exists {
			return Result{}, fmt.Errorf("duplicate roster column %q", name)
		}
		index[name] = i
	}
	for _, name := range []string{"canvas_id", "account_number", "name", "email", "sis_user_id"} {
		if _, ok := index[name]; !ok {
			return Result{}, fmt.Errorf("roster CSV missing required column %q", name)
		}
	}
	result := Result{}
	students := make(map[string]domain.Student)
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Result{}, fmt.Errorf("parse roster CSV: %w", err)
		}
		line, _ := r.FieldPos(0)
		get := func(name string) string { return strings.TrimSpace(row[index[name]]) }
		account := get("account_number")
		if account == "" {
			result.Warnings = append(result.Warnings, Warning{Row: line, Message: "empty account_number; skipped"})
			continue
		}
		if _, exists := students[account]; exists {
			result.Warnings = append(result.Warnings, Warning{Row: line, AccountNumber: account, Message: "duplicate account_number; last row wins"})
		}
		students[account] = domain.Student{AccountNumber: account, CanvasID: get("canvas_id"), Name: get("name"), Email: get("email"), SISUserID: get("sis_user_id")}
	}
	if len(students) == 0 {
		return Result{}, fmt.Errorf("roster contains no usable student accounts")
	}
	accounts := make([]string, 0, len(students))
	for account := range students {
		accounts = append(accounts, account)
	}
	sort.Strings(accounts)
	for _, account := range accounts {
		result.Students = append(result.Students, students[account])
	}
	return result, nil
}

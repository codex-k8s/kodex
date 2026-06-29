package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const codexLimitProgressBarWidth = 8

type codexLimitsSnapshot struct {
	LimitID   string
	LimitName string
	Primary   *codexRateLimitWindow
	Secondary *codexRateLimitWindow
	Credits   json.RawMessage
	PlanType  string
}

type codexRateLimitWindow struct {
	UsedPercent   *float64
	WindowMinutes *int64
	ResetsAt      *int64
}

type codexLimitEventEnvelope struct {
	Timestamp string                 `json:"timestamp"`
	Payload   codexLimitEventPayload `json:"payload"`
}

type codexLimitEventPayload struct {
	Type       string               `json:"type"`
	RateLimits *codexLimitsSnapshot `json:"rate_limits"`
}

type codexLimitSnapshotCandidate struct {
	Timestamp string
	Snapshot  codexLimitsSnapshot
}

type codexLimitFileCandidate struct {
	Path    string
	ModTime time.Time
}

func latestCodexLimitsSummary(codexHome string) (string, error) {
	snapshot, err := findLatestCodexLimitsSnapshot(codexHome)
	if err != nil || snapshot == nil {
		return "", err
	}
	return formatCodexLimitsInline(*snapshot), nil
}

func findLatestCodexLimitsSnapshot(codexHome string) (*codexLimitsSnapshot, error) {
	var files []codexLimitFileCandidate
	for _, dir := range []string{"sessions", "archived_sessions"} {
		items, err := collectCodexLimitJSONLFiles(filepath.Join(codexHome, dir))
		if err != nil {
			return nil, err
		}
		files = append(files, items...)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime.After(files[j].ModTime)
	})
	var snapshots []codexLimitSnapshotCandidate
	for i, file := range files {
		if i >= 200 {
			break
		}
		items, err := parseCodexLimitSnapshots(file.Path)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, items...)
	}
	return selectAuthoritativeCodexLimitsSnapshot(snapshots), nil
}

func collectCodexLimitJSONLFiles(root string) ([]codexLimitFileCandidate, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	var files []codexLimitFileCandidate
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, codexLimitFileCandidate{Path: path, ModTime: info.ModTime()})
		return nil
	})
	return files, err
}

func parseCodexLimitSnapshots(path string) ([]codexLimitSnapshotCandidate, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var snapshots []codexLimitSnapshotCandidate
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var envelope codexLimitEventEnvelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
		}
		if envelope.Payload.Type != "token_count" || envelope.Payload.RateLimits == nil {
			continue
		}
		snapshots = append(snapshots, codexLimitSnapshotCandidate{
			Timestamp: envelope.Timestamp,
			Snapshot:  *envelope.Payload.RateLimits,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return snapshots, nil
}

func selectAuthoritativeCodexLimitsSnapshot(snapshots []codexLimitSnapshotCandidate) *codexLimitsSnapshot {
	if len(snapshots) == 0 {
		return nil
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp > snapshots[j].Timestamp
	})
	newest := snapshots[0].Snapshot
	primaryReset := codexWindowReset(newest.Primary)
	secondaryReset := codexWindowReset(newest.Secondary)
	var sameWindow []codexLimitsSnapshot
	for _, candidate := range snapshots {
		if codexWindowReset(candidate.Snapshot.Primary) == primaryReset &&
			codexWindowReset(candidate.Snapshot.Secondary) == secondaryReset {
			sameWindow = append(sameWindow, candidate.Snapshot)
		}
	}
	merged := mergeCodexLimitsSnapshots(sameWindow, newest)
	return &merged
}

func mergeCodexLimitsSnapshots(snapshots []codexLimitsSnapshot, fallback codexLimitsSnapshot) codexLimitsSnapshot {
	merged := fallback
	var primary []*codexRateLimitWindow
	var secondary []*codexRateLimitWindow
	for _, snapshot := range snapshots {
		if snapshot.Primary != nil {
			primary = append(primary, snapshot.Primary)
		}
		if snapshot.Secondary != nil {
			secondary = append(secondary, snapshot.Secondary)
		}
	}
	merged.Primary = mergeCodexLimitWindow(primary, merged.Primary)
	merged.Secondary = mergeCodexLimitWindow(secondary, merged.Secondary)
	return merged
}

func mergeCodexLimitWindow(windows []*codexRateLimitWindow, fallback *codexRateLimitWindow) *codexRateLimitWindow {
	if fallback == nil {
		return nil
	}
	merged := *fallback
	for _, window := range windows {
		if window == nil {
			continue
		}
		if merged.UsedPercent == nil && window.UsedPercent != nil {
			merged.UsedPercent = window.UsedPercent
		}
		if merged.UsedPercent != nil && window.UsedPercent != nil && *window.UsedPercent > *merged.UsedPercent {
			merged.UsedPercent = window.UsedPercent
		}
		if merged.WindowMinutes == nil && window.WindowMinutes != nil {
			merged.WindowMinutes = window.WindowMinutes
		}
		if merged.ResetsAt == nil && window.ResetsAt != nil {
			merged.ResetsAt = window.ResetsAt
		}
	}
	return &merged
}

func formatCodexLimitsInline(snapshot codexLimitsSnapshot) string {
	var lines []string
	if line := formatCodexLimitWindow("5h", snapshot.Primary); line != "" {
		lines = append(lines, line)
	}
	if line := formatCodexLimitWindow("7d", snapshot.Secondary); line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func formatCodexLimitWindow(label string, window *codexRateLimitWindow) string {
	if window == nil || window.UsedPercent == nil {
		return ""
	}
	remaining := remainingCodexLimitPercent(*window.UsedPercent)
	icon := "•"
	switch label {
	case "5h":
		icon = "🕔"
	case "7d":
		icon = "📅"
	}
	return fmt.Sprintf("%s %-2s %s %3.0f%% · %s", icon, label, formatCodexLimitProgressBar(remaining), remaining, formatCodexLimitReset(window.ResetsAt))
}

func remainingCodexLimitPercent(usedPercent float64) float64 {
	remaining := 100 - usedPercent
	if remaining < 0 {
		return 0
	}
	if remaining > 100 {
		return 100
	}
	return remaining
}

func formatCodexLimitProgressBar(remainingPercent float64) string {
	filled := int((remainingPercent/100)*codexLimitProgressBarWidth + 0.5)
	if filled < 0 {
		filled = 0
	}
	if filled > codexLimitProgressBarWidth {
		filled = codexLimitProgressBarWidth
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", codexLimitProgressBarWidth-filled)
}

func formatCodexLimitReset(timestamp *int64) string {
	if timestamp == nil {
		return "unknown"
	}
	return time.Unix(*timestamp, 0).In(time.Local).Format("02.01 15:04")
}

func codexWindowReset(window *codexRateLimitWindow) int64 {
	if window == nil || window.ResetsAt == nil {
		return 0
	}
	return *window.ResetsAt
}

func (snapshot *codexLimitsSnapshot) UnmarshalJSON(data []byte) error {
	type rawSnapshot struct {
		LimitID        string                `json:"limitId"`
		LimitIDSnake   string                `json:"limit_id"`
		LimitName      string                `json:"limitName"`
		LimitNameSnake string                `json:"limit_name"`
		Primary        *codexRateLimitWindow `json:"primary"`
		Secondary      *codexRateLimitWindow `json:"secondary"`
		Credits        json.RawMessage       `json:"credits"`
		PlanType       string                `json:"planType"`
		PlanTypeSnake  string                `json:"plan_type"`
	}
	var raw rawSnapshot
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	snapshot.LimitID = defaultString(raw.LimitID, raw.LimitIDSnake)
	snapshot.LimitName = defaultString(raw.LimitName, raw.LimitNameSnake)
	snapshot.Primary = raw.Primary
	snapshot.Secondary = raw.Secondary
	snapshot.Credits = raw.Credits
	snapshot.PlanType = defaultString(raw.PlanType, raw.PlanTypeSnake)
	return nil
}

func (payload *codexLimitEventPayload) UnmarshalJSON(data []byte) error {
	type rawPayload struct {
		Type            string               `json:"type"`
		RateLimits      *codexLimitsSnapshot `json:"rate_limits"`
		RateLimitsCamel *codexLimitsSnapshot `json:"rateLimits"`
	}
	var raw rawPayload
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	payload.Type = raw.Type
	payload.RateLimits = raw.RateLimits
	if payload.RateLimits == nil {
		payload.RateLimits = raw.RateLimitsCamel
	}
	return nil
}

func (window *codexRateLimitWindow) UnmarshalJSON(data []byte) error {
	type rawWindow struct {
		UsedPercent        *float64 `json:"usedPercent"`
		UsedPercentSnake   *float64 `json:"used_percent"`
		WindowDurationMins *int64   `json:"windowDurationMins"`
		WindowMinutes      *int64   `json:"window_minutes"`
		ResetsAt           *int64   `json:"resetsAt"`
		ResetsAtSnake      *int64   `json:"resets_at"`
	}
	var raw rawWindow
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	window.UsedPercent = firstFloat64(raw.UsedPercent, raw.UsedPercentSnake)
	window.WindowMinutes = firstInt64(raw.WindowDurationMins, raw.WindowMinutes)
	window.ResetsAt = firstInt64(raw.ResetsAt, raw.ResetsAtSnake)
	return nil
}

func firstFloat64(values ...*float64) *float64 {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstInt64(values ...*int64) *int64 {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

package mattermost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mattermost/mattermost/server/public/model"
)

type operationInput struct {
	PostID  string `json:"post_id"`
	RootID  string `json:"root_id"`
	FileID  string `json:"file_id"`
	Message string `json:"message"`
	Query   string `json:"query"`
	Emoji   string `json:"emoji_name"`
	Cursor  string `json:"cursor"`
	Page    int    `json:"page"`
	Limit   int    `json:"limit"`
	Offset  int64  `json:"offset"`
}

func executeOperation(ctx context.Context, client *model.Client4, channel *model.Channel, operation string, input operationInput) (map[string]any, string, bool, error) {
	failed := func(err error) (map[string]any, string, bool, error) { return nil, "", false, err }
	if channel == nil || !model.IsValidId(channel.Id) || !model.IsValidId(channel.TeamId) {
		return failed(errResponse)
	}
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}
	if input.Page < 0 || input.Page > 10000 || (operation != "mattermost.file.read" && (limit < 1 || limit > 50)) {
		return failed(errInvocation)
	}
	switch operation {
	case "mattermost.team.read":
		team, _, err := client.GetTeam(ctx, channel.TeamId, "")
		if err != nil {
			return failed(classify(err))
		}
		if team == nil || team.Id != channel.TeamId || team.DeleteAt != 0 {
			return failed(errResponse)
		}
		return map[string]any{"id": team.Id, "name": team.Name, "display_name": team.DisplayName}, team.Id, false, nil
	case "mattermost.channel.read":
		result := map[string]any{"id": channel.Id, "team_id": channel.TeamId, "name": channel.Name, "display_name": channel.DisplayName}
		if channel.Purpose != "" {
			result["purpose"] = channel.Purpose
		}
		return result, channel.Id, false, nil
	case "mattermost.channel.members.list":
		members, _, err := client.GetChannelMembers(ctx, channel.Id, input.Page, limit, "")
		if err != nil {
			return failed(classify(err))
		}
		if len(members) > limit {
			return failed(errResponse)
		}
		items := make([]map[string]any, 0, len(members))
		for _, member := range members {
			if member.ChannelId != channel.Id || !model.IsValidId(member.UserId) {
				return failed(errResponse)
			}
			items = append(items, map[string]any{"user_id": member.UserId, "roles": member.Roles})
		}
		result, err := listProjection("members", items)
		if err != nil {
			return failed(err)
		}
		if len(items) == limit && input.Page < 10000 {
			result["next_page"] = input.Page + 1
		}
		return result, channel.Id, false, nil
	case "mattermost.post.list", "mattermost.post.search", "mattermost.thread.read":
		return readPosts(ctx, client, channel, operation, input, limit)
	case "mattermost.post.read":
		post, err := scopedPost(ctx, client, channel.Id, input.PostID)
		if err != nil {
			return failed(err)
		}
		return postProjection(post, false), post.Id, false, nil
	case "mattermost.file.list", "mattermost.file.read":
		return readFiles(ctx, client, channel, operation, input)
	case "mattermost.reaction.list":
		post, err := scopedPost(ctx, client, channel.Id, input.PostID)
		if err != nil {
			return failed(err)
		}
		reactions, _, err := client.GetReactions(ctx, post.Id)
		if err != nil {
			return failed(classify(err))
		}
		if len(reactions) > 1000 {
			return failed(errResponse)
		}
		items := make([]map[string]any, 0, len(reactions))
		for _, reaction := range reactions {
			if reaction == nil || reaction.PostId != post.Id || !model.IsValidId(reaction.UserId) || !validEmoji(reaction.EmojiName) {
				return failed(errResponse)
			}
			items = append(items, map[string]any{"post_id": post.Id, "user_id": reaction.UserId, "emoji_name": reaction.EmojiName})
		}
		result, err := listProjection("reactions", items)
		return result, post.Id, false, err
	case "mattermost.post.send", "mattermost.notifications", "mattermost.result_mirror":
		if strings.TrimSpace(input.Message) == "" || len(input.Message) > maximumMessageBytes {
			return failed(errInvocation)
		}
		if input.RootID != "" {
			root, err := scopedPost(ctx, client, channel.Id, input.RootID)
			if err != nil {
				return failed(err)
			}
			if root.RootId != "" {
				return failed(errInvocation)
			}
		}
		post, _, err := createPost(ctx, client, channel.Id, input.RootID, input.Message, nil)
		return map[string]any{"message_id": post}, post, true, err
	case "mattermost.post.update", "mattermost.reaction.add", "mattermost.reaction.remove":
		return changePost(ctx, client, channel, operation, input)
	default:
		return failed(errInvocation)
	}
}

func scopedPost(ctx context.Context, client *model.Client4, channelID, postID string) (*model.Post, error) {
	if !model.IsValidId(postID) {
		return nil, errInvocation
	}
	post, _, err := client.GetPost(ctx, postID, "")
	if err != nil {
		return nil, classify(err)
	}
	if !validReadPost(post, channelID) || post.Id != postID {
		return nil, errResponse
	}
	return post, nil
}

func validReadPost(post *model.Post, channelID string) bool {
	return post != nil && model.IsValidId(post.Id) && model.IsValidId(post.UserId) && post.ChannelId == channelID && post.DeleteAt == 0 && post.UpdateAt >= 0 && (post.RootId == "" || model.IsValidId(post.RootId)) && len(post.Message) <= maximumMessageBytes
}

func postProjection(post *model.Post, preview bool) map[string]any {
	message := post.Message
	if preview && len(message) > 512 {
		message = message[:512]
		for !utf8.ValidString(message) {
			message = message[:len(message)-1]
		}
	}
	result := map[string]any{"id": post.Id, "channel_id": post.ChannelId, "user_id": post.UserId, "root_id": post.RootId, "message": message, "updated_at": post.UpdateAt}
	if post.RootId == "" {
		delete(result, "root_id")
	}
	if message == "" {
		delete(result, "message")
	}
	if preview {
		result["message_truncated"] = message != post.Message
	}
	return result
}

func listProjection(key string, items []map[string]any) (map[string]any, error) {
	body, err := json.Marshal(items)
	if err != nil || len(body) > 60000 {
		return nil, errResponse
	}
	return map[string]any{key: string(body), "count": len(items)}, nil
}

func readPosts(ctx context.Context, client *model.Client4, channel *model.Channel, operation string, input operationInput, limit int) (map[string]any, string, bool, error) {
	var posts *model.PostList
	var err error
	rootID := ""
	switch operation {
	case "mattermost.post.list":
		posts, _, err = client.GetPostsForChannel(ctx, channel.Id, input.Page, limit, "", true, false)
	case "mattermost.post.search":
		query := strings.TrimSpace(input.Query)
		if query == "" || len(query) > 256 || strings.ContainsAny(query, ":\"\\\r\n") {
			return nil, "", false, errInvocation
		}
		terms := "in:" + channel.Name + " \"" + query + "\""
		isOr, includeDeleted := false, false
		posts, _, err = client.SearchPostsWithParams(ctx, channel.TeamId, &model.SearchParameter{Terms: &terms, IsOrSearch: &isOr, Page: &input.Page, PerPage: &limit, IncludeDeletedChannels: &includeDeleted})
	case "mattermost.thread.read":
		root, readErr := scopedPost(ctx, client, channel.Id, input.PostID)
		if readErr != nil {
			return nil, "", false, readErr
		}
		rootID = root.Id
		if root.RootId != "" {
			rootID = root.RootId
		}
		if input.Cursor != "" {
			cursor, readErr := scopedPost(ctx, client, channel.Id, input.Cursor)
			if readErr != nil {
				return nil, "", false, readErr
			}
			if cursor.Id != rootID && cursor.RootId != rootID {
				return nil, "", false, errInvocation
			}
		}
		posts, _, err = client.GetPostThreadWithOpts(ctx, rootID, "", model.GetPostsOptions{PerPage: limit, FromPost: input.Cursor, Direction: "down"})
	}
	if err != nil {
		return nil, "", false, classify(err)
	}
	if posts == nil || len(posts.Order) > limit || len(posts.Posts) > 1000 {
		return nil, "", false, errResponse
	}
	for key, post := range posts.Posts {
		if !validReadPost(post, channel.Id) || key != post.Id || (rootID != "" && post.Id != rootID && post.RootId != rootID) {
			return nil, "", false, errResponse
		}
	}
	items := make([]map[string]any, 0, len(posts.Order))
	seen := map[string]bool{}
	for _, id := range posts.Order {
		post := posts.Posts[id]
		if post == nil || seen[id] {
			return nil, "", false, errResponse
		}
		seen[id] = true
		items = append(items, postProjection(post, true))
	}
	result, err := listProjection("posts", items)
	if err != nil {
		return nil, "", false, err
	}
	if len(items) == limit && len(posts.Order) > 0 {
		if operation == "mattermost.thread.read" {
			result["next_cursor"] = posts.Order[len(posts.Order)-1]
		} else if input.Page < 10000 {
			result["next_page"] = input.Page + 1
		}
	}
	return result, channel.Id, false, nil
}

func readFiles(ctx context.Context, client *model.Client4, channel *model.Channel, operation string, input operationInput) (map[string]any, string, bool, error) {
	post, err := scopedPost(ctx, client, channel.Id, input.PostID)
	if err != nil {
		return nil, "", false, err
	}
	if len(post.FileIds) > 50 {
		return nil, "", false, errResponse
	}
	if operation == "mattermost.file.list" {
		files, _, err := client.GetFileInfosForPost(ctx, post.Id, "")
		if err != nil {
			return nil, "", false, classify(err)
		}
		if len(files) > 50 {
			return nil, "", false, errResponse
		}
		items := make([]map[string]any, 0, len(files))
		for _, file := range files {
			if !validFile(file, post) {
				return nil, "", false, errResponse
			}
			items = append(items, map[string]any{"id": file.Id, "name": file.Name, "size": file.Size, "mime_type": file.MimeType})
		}
		result, err := listProjection("files", items)
		return result, post.Id, false, err
	}
	if !model.IsValidId(input.FileID) || !slices.Contains(post.FileIds, input.FileID) {
		return nil, "", false, errInvocation
	}
	file, _, err := client.GetFileInfo(ctx, input.FileID)
	if err != nil {
		return nil, "", false, classify(err)
	}
	if !validFile(file, post) || file.Id != input.FileID {
		return nil, "", false, errResponse
	}
	limit := input.Limit
	if limit == 0 {
		limit = 32768
	}
	if limit < 1 || limit > 32768 || input.Offset < 0 || input.Offset > file.Size || (input.Offset == file.Size && file.Size != 0) {
		return nil, "", false, errInvocation
	}
	length := min(int64(limit), file.Size-input.Offset)
	var body []byte
	if length > 0 {
		end := input.Offset + length - 1
		response, requestErr := client.DoAPIRequestWithHeaders(ctx, http.MethodGet, "/files/"+file.Id, "", map[string]string{"Range": fmt.Sprintf("bytes=%d-%d", input.Offset, end)})
		if requestErr != nil {
			return nil, "", false, classify(requestErr)
		}
		if response == nil || response.Body == nil {
			return nil, "", false, errResponse
		}
		defer response.Body.Close()
		exactRange := response.StatusCode == http.StatusPartialContent && response.Header.Get("Content-Range") == fmt.Sprintf("bytes %d-%d/%d", input.Offset, end, file.Size)
		wholeFile := response.StatusCode == http.StatusOK && input.Offset == 0 && length == file.Size
		if !exactRange && !wholeFile {
			return nil, "", false, errResponse
		}
		body, err = io.ReadAll(io.LimitReader(response.Body, length+1))
		if err != nil || int64(len(body)) != length {
			return nil, "", false, errResponse
		}
	}
	result := map[string]any{"id": file.Id, "name": file.Name, "content": base64.StdEncoding.EncodeToString(body), "encoding": "base64", "offset": input.Offset, "total_bytes": file.Size}
	if len(body) == 0 {
		delete(result, "content")
	}
	if next := input.Offset + length; next < file.Size {
		result["next_offset"] = next
	}
	return result, file.Id, false, nil
}

func validFile(file *model.FileInfo, post *model.Post) bool {
	return file != nil && model.IsValidId(file.Id) && file.PostId == post.Id && (file.ChannelId == "" || file.ChannelId == post.ChannelId) && slices.Contains(post.FileIds, file.Id) && file.DeleteAt == 0 && file.Size >= 0 && file.Size <= 1<<30 && len(file.Name) <= 512 && len(file.MimeType) <= 128
}

func changePost(ctx context.Context, client *model.Client4, channel *model.Channel, operation string, input operationInput) (map[string]any, string, bool, error) {
	post, err := scopedPost(ctx, client, channel.Id, input.PostID)
	if err != nil {
		return nil, "", false, err
	}
	me, _, err := client.GetMe(ctx, "")
	if err != nil {
		return nil, "", false, classify(err)
	}
	if me == nil || !model.IsValidId(me.Id) {
		return nil, "", false, errResponse
	}
	if operation == "mattermost.post.update" {
		if post.UserId != me.Id || post.GetProp(gateRefProperty) != nil {
			return nil, "", false, errForbidden
		}
		if strings.TrimSpace(input.Message) == "" || len(input.Message) > maximumMessageBytes {
			return nil, "", false, errInvocation
		}
		updated, response, err := client.PatchPost(ctx, post.Id, &model.PostPatch{Message: &input.Message})
		if err != nil {
			return nil, "", true, mutationError(response, err)
		}
		if !validReadPost(updated, channel.Id) || updated.Id != post.Id || updated.UserId != me.Id || updated.Message != input.Message || updated.RootId != post.RootId {
			return nil, "", true, errResponse
		}
		return map[string]any{"message_id": updated.Id}, updated.Id, true, nil
	}
	if !validEmoji(input.Emoji) {
		return nil, "", false, errInvocation
	}
	reaction := &model.Reaction{PostId: post.Id, UserId: me.Id, EmojiName: input.Emoji}
	if operation == "mattermost.reaction.add" {
		created, response, err := client.SaveReaction(ctx, reaction)
		if err != nil {
			return nil, "", true, mutationError(response, err)
		}
		if created == nil || created.PostId != reaction.PostId || created.UserId != reaction.UserId || created.EmojiName != reaction.EmojiName || created.DeleteAt != 0 {
			return nil, "", true, errResponse
		}
	} else {
		response, err := client.DeleteReaction(ctx, reaction)
		if err != nil {
			return nil, "", true, mutationError(response, err)
		}
		if response == nil || response.StatusCode != http.StatusOK {
			return nil, "", true, errResponse
		}
	}
	return map[string]any{"status": "applied"}, post.Id + ":" + me.Id + ":" + input.Emoji, true, nil
}

func validEmoji(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '+') {
			return false
		}
	}
	return true
}

func mutationError(response *model.Response, err error) error {
	classified := classify(err)
	if response != nil {
		switch response.StatusCode {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusTooManyRequests:
			return &noEffectError{cause: classified}
		}
	}
	return classified
}

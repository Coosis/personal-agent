package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Coosis/personal-agent/internal/agenthttp"
	"github.com/Coosis/personal-agent/internal/apiutil"
	"github.com/Coosis/personal-agent/internal/db"
	"github.com/Coosis/personal-agent/internal/jobqueue"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

var (
	ErrNotFound         = errors.New("conversation not found")
	ErrAgentUnavailable = errors.New("agent unavailable")
)

type Service struct {
	db    *db.DB
	agent *agenthttp.Client
}

type preparedMessages struct {
	user      sqlc.Message
	assistant sqlc.Message
	run       sqlc.AgentRun
	history   []agenthttp.ChatMessage
}

func NewService(database *db.DB, agent *agenthttp.Client) *Service {
	return &Service{db: database, agent: agent}
}

func (s *Service) List(ctx context.Context, req ListRequest) ([]Conversation, error) {
	rows, err := s.db.Queries.ListConversations(ctx, sqlc.ListConversationsParams{
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, err
	}

	items := make([]Conversation, 0, len(rows))
	for _, row := range rows {
		items = append(items, toConversation(row))
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*Conversation, error) {
	row, err := s.db.Queries.GetConversationByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	item := toConversation(row)
	return &item, nil
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Conversation, error) {
	row, err := s.db.Queries.CreateConversation(ctx, sqlc.CreateConversationParams{
		Title:    apiutil.Text(req.Title),
		Summary:  pgtype.Text{},
		Metadata: apiutil.DefaultJSONObject(),
	})
	if err != nil {
		return nil, err
	}

	item := toConversation(row)
	return &item, nil
}

func (s *Service) ListMessages(ctx context.Context, id int64, req ListMessagesRequest) ([]Message, error) {
	if _, err := s.db.Queries.GetConversationByID(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	rows, err := s.db.Queries.ListMessagesByConversation(ctx, sqlc.ListMessagesByConversationParams{
		ConversationID: id,
		Limit:          req.Limit,
		Offset:         req.Offset,
	})
	if err != nil {
		return nil, err
	}

	items := make([]Message, 0, len(rows))
	for _, row := range rows {
		items = append(items, toMessage(row))
	}
	return items, nil
}

func (s *Service) SendMessage(ctx context.Context, conversationID int64, req SendMessageRequest) (*SendMessageResponse, error) {
	// db stuff
	prepared, err := s.prepareMessages(ctx, conversationID, req)
	if err != nil {
		return nil, err
	}

	// streaming
	assistantRow, err := s.runAssistantReplyStream(ctx, prepared.assistant, prepared.run, req.Content, prepared.history, nil)
	response := &SendMessageResponse{
		UserMessage:      toMessage(prepared.user),
		AssistantMessage: toMessage(assistantRow),
	}
	if err != nil {
		return response, err
	}
	return response, nil
}

// insert messages into db in single tx started within
func (s *Service) prepareMessages(
	ctx context.Context,
	conversationID int64,
	req SendMessageRequest,
) (*preparedMessages, error) {
	if _, err := s.db.Queries.GetConversationByID(ctx, conversationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if s.agent == nil {
		return nil, ErrAgentUnavailable
	}

	var prepared preparedMessages
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		historyRows, err := q.ListCompletedMessagesByConversation(ctx, conversationID)
		if err != nil {
			return err
		}
		prepared.history = buildAgentConversationHistory(historyRows)

		seq, err := q.GetLatestMessageSequence(ctx, conversationID)
		if err != nil {
			return err
		}

		prepared.user, err = q.CreateMessage(ctx, sqlc.CreateMessageParams{
			ConversationID:  conversationID,
			Role:            "user",
			Status:          "completed",
			Content:         req.Content,
			Citations:       apiutil.DefaultJSONArray(),
			ToolCalls:       apiutil.DefaultJSONArray(),
			ToolResults:     apiutil.DefaultJSONArray(),
			TokenCount:      pgtype.Int4{},
			LatencyMs:       pgtype.Int4{},
			Model:           pgtype.Text{},
			ParentMessageID: pgtype.Int8{},
			SequenceNumber:  seq + 1,
			Metadata:        apiutil.DefaultJSONObject(),
		})
		if err != nil {
			return err
		}

		if _, err := jobqueue.Enqueue(ctx, q, "extract_memory_suggestions", map[string]any{
			"message_id":      prepared.user.ID,
			"conversation_id": conversationID,
			"sequence_number": prepared.user.SequenceNumber,
			"role":            prepared.user.Role,
			"trigger":         "message_create",
		}, jobqueue.ExtractMemorySuggestionsFromMessageDedupeKey(prepared.user.ID)); err != nil {
			return err
		}

		prepared.assistant, err = q.CreateMessage(ctx, sqlc.CreateMessageParams{
			ConversationID:  conversationID,
			Role:            "assistant",
			Status:          "streaming",
			Content:         "",
			Citations:       apiutil.DefaultJSONArray(),
			ToolCalls:       apiutil.DefaultJSONArray(),
			ToolResults:     apiutil.DefaultJSONArray(),
			TokenCount:      pgtype.Int4{},
			LatencyMs:       pgtype.Int4{},
			Model:           pgtype.Text{},
			ParentMessageID: pgtype.Int8{Int64: prepared.user.ID, Valid: true},
			SequenceNumber:  seq + 2,
			Metadata:        apiutil.DefaultJSONObject(),
		})
		if err != nil {
			return err
		}

		prepared.run, err = q.CreateAgentRun(ctx, sqlc.CreateAgentRunParams{
			ConversationID:   apiutil.Int8(conversationID),
			TriggerMessageID: apiutil.Int8(prepared.user.ID),
			Status:           "running",
			Metadata: apiutil.MarshalJSON(map[string]any{
				"assistant_message_id":  prepared.assistant.ID,
				"history_message_count": len(prepared.history),
				"request_transport":     "conversation_message",
			}),
		})
		return err
	})
	if err != nil {
		return nil, err
	}

	return &prepared, nil
}

func (s *Service) runAssistantReplyStream(
	ctx context.Context,
	assistant sqlc.Message,
	run sqlc.AgentRun,
	content string,
	history []agenthttp.ChatMessage,
	onToken func(string) error,
) (sqlc.Message, error) {
	startedAt := time.Now()
	var builder strings.Builder
	streamResult := agenthttp.ChatStreamResult{
		Citations: apiutil.DefaultJSONArray(),
		ToolsUsed: []string{},
	}

	// streaming part
	result, err := s.agent.ChatStream(ctx, content, history, func(token string) error {
		builder.WriteString(token)
		if onToken == nil {
			return nil
		}
		return onToken(token)
	})
	if err == nil {
		streamResult = result
	}
	// streaming finished, clean up phase

	finalizeCtx := context.WithoutCancel(ctx)
	finishedAt := time.Now()
	finalStatus := "completed"
	errorType := pgtype.Text{}
	errorMessage := pgtype.Text{}
	if err != nil {
		finalStatus = classifyRunStatus(err)
		errorType = apiutil.Text(classifyRunErrorType(err))
		errorMessage = apiutil.Text(err.Error())
	}

	finalMessageStatus := "completed"
	if err != nil {
		finalMessageStatus = "failed"
	}

	documentsAccessed := citationDocumentIDs(streamResult.Citations)
	trace := buildAgentRunTrace(run, assistant, startedAt, finishedAt, streamResult, documentsAccessed, builder.Len(), err)
	updatedMetadata := mergeJSONMetadata(run.Metadata, map[string]any{
		"assistant_status": finalMessageStatus,
		"citation_count":   citationCount(streamResult.Citations),
		"response_chars":   builder.Len(),
	})

	var updated sqlc.Message
	updateErr := s.db.WithTx(finalizeCtx, func(q *sqlc.Queries) error {
		msg, err := q.UpdateMessageFinal(finalizeCtx, sqlc.UpdateMessageFinalParams{
			ID:        assistant.ID,
			Content:   builder.String(),
			Status:    finalMessageStatus,
			Citations: streamResult.Citations,
		})
		if err != nil {
			return err
		}
		updated = msg

		_, err = q.UpdateAgentRun(finalizeCtx, sqlc.UpdateAgentRunParams{
			ID:                run.ID,
			Status:            finalStatus,
			Trace:             trace,
			ToolsUsed:         dedupeStrings(streamResult.ToolsUsed),
			DocumentsAccessed: documentsAccessed,
			EndTime:           pgtype.Timestamptz{Time: finishedAt, Valid: true},
			TotalTokens:       0,
			TotalLatencyMs:    int32(finishedAt.Sub(startedAt).Milliseconds()),
			StepCount:         streamResult.StepCount,
			ErrorType:         errorType,
			ErrorMessage:      errorMessage,
			Metadata:          updatedMetadata,
		})
		if err != nil {
			return err
		}

		if finalMessageStatus != "completed" {
			return nil
		}

		passIndex, err := q.CountCompletedAssistantMessagesByConversation(finalizeCtx, assistant.ConversationID)
		if err != nil {
			return err
		}
		if !shouldEmitConversationSummary(passIndex) {
			return nil
		}

		_, err = jobqueue.Enqueue(finalizeCtx, q, "summarize_conversation", map[string]any{
			"conversation_id":    assistant.ConversationID,
			"up_to_message_id":   assistant.ID,
			"pass_index":         passIndex,
			"trigger_message_id": apiutil.Int64Ptr(run.TriggerMessageID),
			"agent_run_id":       run.ID,
		}, jobqueue.SummarizeConversationDedupeKey(assistant.ConversationID, passIndex))
		return err
	})
	if updateErr != nil {
		if err != nil {
			return assistant, fmt.Errorf("agent stream failed: %w; additionally failed to finalize records: %v", err, updateErr)
		}
		return assistant, updateErr
	}

	if err != nil {
		return updated, err
	}
	return updated, nil
}

func classifyRunStatus(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "failed"
}

func shouldEmitConversationSummary(passIndex int32) bool {
	return passIndex > 0 && (passIndex-1)%3 == 0
}

func classifyRunErrorType(err error) string {
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return "agent_stream_error"
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	seen := make(map[string]struct{}, len(values))
	deduped := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		deduped = append(deduped, value)
	}
	return deduped
}

func citationDocumentIDs(raw []byte) []int64 {
	type citation struct {
		DocumentID *int64 `json:"document_id"`
	}

	var payload []citation
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return []int64{}
	}

	seen := map[int64]struct{}{}
	ids := make([]int64, 0, len(payload))
	for _, item := range payload {
		if item.DocumentID == nil {
			continue
		}
		if _, ok := seen[*item.DocumentID]; ok {
			continue
		}
		seen[*item.DocumentID] = struct{}{}
		ids = append(ids, *item.DocumentID)
	}
	return ids
}

func citationCount(raw []byte) int {
	var payload []map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return 0
	}
	return len(payload)
}

func mergeJSONMetadata(raw []byte, updates map[string]any) []byte {
	current := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &current)
	}
	maps.Copy(current, updates)
	return apiutil.MarshalJSON(current)
}

func buildAgentRunTrace(
	run sqlc.AgentRun,
	assistant sqlc.Message,
	startedAt time.Time,
	finishedAt time.Time,
	result agenthttp.ChatStreamResult,
	documentsAccessed []int64,
	responseChars int,
	streamErr error,
) []byte {
	events := []map[string]any{
		{
			"timestamp":            startedAt.UTC().Format(time.RFC3339Nano),
			"phase":                "start",
			"conversation_id":      apiutil.Int64Ptr(run.ConversationID),
			"trigger_message_id":   apiutil.Int64Ptr(run.TriggerMessageID),
			"assistant_message_id": assistant.ID,
		},
		{
			"timestamp": finishedAt.UTC().Format(time.RFC3339Nano),
			"phase":     "finish",
			"status": func() string {
				if streamErr != nil {
					return classifyRunStatus(streamErr)
				}
				return "completed"
			}(),
			"step_count":         result.StepCount,
			"tools_used":         dedupeStrings(result.ToolsUsed),
			"documents_accessed": documentsAccessed,
			"citations":          json.RawMessage(result.Citations),
			"latency_ms":         finishedAt.Sub(startedAt).Milliseconds(),
			"response_chars":     responseChars,
		},
	}
	if streamErr != nil {
		events[1]["error_type"] = classifyRunErrorType(streamErr)
		events[1]["error_message"] = streamErr.Error()
	}
	return apiutil.MarshalJSON(events)
}

func buildAgentConversationHistory(rows []sqlc.Message) []agenthttp.ChatMessage {
	history := make([]agenthttp.ChatMessage, 0, len(rows))
	for _, row := range rows {
		if row.Role != "user" && row.Role != "assistant" && row.Role != "system" && row.Role != "tool" {
			continue
		}
		history = append(history, agenthttp.ChatMessage{
			Role:    row.Role,
			Content: row.Content,
		})
	}
	return history
}

// sqlc convenience functions

func toConversation(row sqlc.Conversation) Conversation {
	return Conversation{
		ID:        row.ID,
		Title:     apiutil.TextPtr(row.Title),
		Summary:   apiutil.TextPtr(row.Summary),
		Metadata:  row.Metadata,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

func toMessage(row sqlc.Message) Message {
	return Message{
		ID:             row.ID,
		ConversationID: row.ConversationID,
		Role:           row.Role,
		Status:         row.Status,
		Content:        row.Content,
		Citations:      row.Citations,
		ToolCalls:      row.ToolCalls,
		ToolResults:    row.ToolResults,
		TokenCount:     apiutil.Int32Ptr(row.TokenCount),
		LatencyMs:      apiutil.Int32Ptr(row.LatencyMs),
		Model:          apiutil.TextPtr(row.Model),
		SequenceNumber: row.SequenceNumber,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}
}

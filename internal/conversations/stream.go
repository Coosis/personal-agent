package conversations

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/Coosis/personal-agent/internal/apiutil"
)

type streamResult struct {
	assistant Message
	err       error
}

type streamEvent struct {
	Type           string          `json:"type"`
	Content        string          `json:"content,omitempty"`
	Citations      json.RawMessage `json:"citations,omitempty"`
	UserMessageID  int64           `json:"user_message_id,omitempty"`
	AssistantMsgID int64           `json:"assistant_message_id,omitempty"`
	AgentRunID     int64           `json:"agent_run_id,omitempty"`
	Error          string          `json:"error,omitempty"`
}

type sseWriter struct {
	writer  http.ResponseWriter
	flusher http.Flusher
}

func newSSEWriter(w http.ResponseWriter) (*sseWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("response writer does not support streaming")
	}
	return &sseWriter{writer: w, flusher: flusher}, nil
}

func (w *sseWriter) write(event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal sse payload: %w", err)
	}
	if _, err := fmt.Fprintf(w.writer, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w.writer, "data: %s\n\n", data); err != nil {
		return err
	}
	w.flusher.Flush()
	return nil
}

func (w *sseWriter) signal(eventType string, result *SendMessageResponse, info streamInfo, errMsg string) error {
	payload := streamEvent{Type: eventType, Error: errMsg}
	if result != nil {
		payload.UserMessageID = result.UserMessage.ID
		payload.AssistantMsgID = result.AssistantMessage.ID
		if len(result.AssistantMessage.Citations) > 0 {
			payload.Citations = json.RawMessage(result.AssistantMessage.Citations)
		}
	}
	payload.AgentRunID = info.AgentRunID
	return w.write("signal", payload)
}

func (w *sseWriter) token(content string) error {
	return w.write("message", streamEvent{
		Type:    "token",
		Content: content,
	})
}

func (h *Handler) StreamMessage(c *gin.Context) {
	id, err := apiutil.ParseIDParam(c, "id")
	if err != nil {
		apiutil.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	var req SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiutil.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	prepared, err := h.service.prepareMessages(c.Request.Context(), id, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			apiutil.Error(c, http.StatusNotFound, "conversation not found")
		case errors.Is(err, ErrAgentUnavailable):
			apiutil.Error(c, http.StatusBadGateway, "agent unavailable")
		default:
			logrus.WithError(err).Error("failed to prepare streamed message")
			apiutil.Error(c, http.StatusInternalServerError, "failed to stream message")
		}
		return
	}

	writer, err := newSSEWriter(c.Writer)
	if err != nil {
		apiutil.Error(c, http.StatusInternalServerError, "streaming not supported")
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	startResponse := &SendMessageResponse{
		UserMessage:      toMessage(prepared.user),
		AssistantMessage: toMessage(prepared.assistant),
	}
	info := streamInfo{AgentRunID: prepared.run.ID}
	if err := writer.signal("start", startResponse, info, ""); err != nil {
		logrus.WithError(err).Error("failed to emit start signal")
		return
	}

	tokenCh := make(chan string, 32)
	resultCh := make(chan streamResult, 1)

	go func() {
		defer close(tokenCh)

		assistantRow, runErr := h.service.runAssistantReplyStream(
			c.Request.Context(),
			prepared.assistant,
			prepared.run,
			req.Content,
			prepared.history,
			func(token string) error {
				select {
				case tokenCh <- token:
					return nil
				case <-c.Request.Context().Done():
					return c.Request.Context().Err()
				}
			})

		resultCh <- streamResult{
			assistant: toMessage(assistantRow),
			err:       runErr,
		}
	}()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case token, ok := <-tokenCh:
			if !ok {
				tokenCh = nil
				continue
			}
			if err := writer.token(token); err != nil {
				logrus.WithError(err).Error("failed to emit token")
				return
			}
		case result := <-resultCh:
			finalResponse := &SendMessageResponse{
				UserMessage:      startResponse.UserMessage,
				AssistantMessage: result.assistant,
			}
			if result.err != nil {
				if emitErr := writer.signal("failed", finalResponse, info, result.err.Error()); emitErr != nil {
					logrus.WithError(emitErr).Error("failed to emit failed signal")
				}
				logrus.WithError(result.err).Error("message stream failed")
				return
			}
			if err := writer.signal("stop", finalResponse, info, ""); err != nil {
				logrus.WithError(err).Error("failed to emit stop signal")
			}
			return
		}
	}
}

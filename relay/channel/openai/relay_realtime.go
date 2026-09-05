package openai

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// realtimeUsageCollector keeps the usage that belongs to the current response
// separate from the usage already finalized by response.done. A provider
// usage record takes precedence over the local estimate for that response.
type realtimeUsageCollector struct {
	mu sync.Mutex

	pending         []dto.RealtimeUsage
	total           dto.RealtimeUsage
	seen            map[string]struct{}
	responseStarted bool
}

func newRealtimeUsageCollector() *realtimeUsageCollector {
	return &realtimeUsageCollector{seen: make(map[string]struct{})}
}

func (c *realtimeUsageCollector) addLocal(textTokens, audioTokens int, input bool) {
	c.addLocalTo(textTokens, audioTokens, input, false)
}

// addResponseLocal adds provider-side fallback tokens to the oldest open
// response. The target reader can observe response.done after the client has
// already opened the next response, so assigning these tokens to the newest
// segment would charge the wrong response.
func (c *realtimeUsageCollector) addResponseLocal(textTokens, audioTokens int, input bool) {
	c.addLocalTo(textTokens, audioTokens, input, true)
}

func (c *realtimeUsageCollector) addLocalTo(textTokens, audioTokens int, input, oldest bool) {
	if c == nil || (textTokens == 0 && audioTokens == 0) {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.pending) == 0 {
		c.pending = append(c.pending, dto.RealtimeUsage{})
	}
	index := len(c.pending) - 1
	if oldest {
		index = 0
	}
	current := &c.pending[index]
	tokens := textTokens + audioTokens
	current.TotalTokens += tokens
	if input {
		current.InputTokens += tokens
		current.InputTokenDetails.TextTokens += textTokens
		current.InputTokenDetails.AudioTokens += audioTokens
		return
	}
	current.OutputTokens += tokens
	current.OutputTokenDetails.TextTokens += textTokens
	current.OutputTokenDetails.AudioTokens += audioTokens
}

// startResponse establishes the first local-estimate segment and opens a new
// one for each subsequent response. Realtime clients may send input for the
// next response before the previous response.done arrives; a queue keeps that
// input separate when the previous response has provider usage that should
// replace, rather than supplement, its local estimate.
func (c *realtimeUsageCollector) startResponse() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if !c.responseStarted {
		c.responseStarted = true
		c.mu.Unlock()
		return
	}
	c.pending = append(c.pending, dto.RealtimeUsage{})
	c.mu.Unlock()
}

// finishResponse adds one response to the final usage. When providerUsage is
// present, pending local estimates are discarded for this response instead of
// being added to the provider's numbers.
func (c *realtimeUsageCollector) finishResponse(eventID string, providerUsage *dto.RealtimeUsage) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.seen == nil {
		c.seen = make(map[string]struct{})
	}
	// An empty event_id is not a usable identity. Keep counting those events
	// independently because a session can contain multiple valid responses and
	// the DTO intentionally does not expose another response identifier.
	if eventID != "" {
		if _, exists := c.seen[eventID]; exists {
			// A duplicate response.done must not charge provider usage or the
			// local fallback estimate a second time. Keep pending usage intact:
			// a late duplicate can arrive after the next response has already
			// started, and clearing it would lose that response's estimate.
			return
		}
		c.seen[eventID] = struct{}{}
	}

	var local dto.RealtimeUsage
	if len(c.pending) > 0 {
		local = c.pending[0]
		c.pending = c.pending[1:]
	}
	if realtimeUsageHasValues(providerUsage) {
		addRealtimeUsage(&c.total, providerUsage)
	} else {
		addRealtimeUsage(&c.total, &local)
	}
}

func realtimeUsageHasValues(usage *dto.RealtimeUsage) bool {
	if usage == nil {
		return false
	}
	return usage.TotalTokens > 0 ||
		usage.InputTokens > 0 ||
		usage.OutputTokens > 0 ||
		usage.InputTokenDetails.AudioTokens > 0 ||
		usage.InputTokenDetails.CachedTokens > 0 ||
		usage.InputTokenDetails.CachedCreationTokens > 0 ||
		usage.InputTokenDetails.CacheWriteTokens > 0 ||
		usage.InputTokenDetails.ImageTokens > 0 ||
		usage.InputTokenDetails.TextTokens > 0 ||
		usage.OutputTokenDetails.AudioTokens > 0 ||
		usage.OutputTokenDetails.ImageTokens > 0 ||
		usage.OutputTokenDetails.ReasoningTokens > 0 ||
		usage.OutputTokenDetails.TextTokens > 0
}

func (c *realtimeUsageCollector) responseSeen(eventID string) bool {
	if c == nil || eventID == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	_, exists := c.seen[eventID]
	return exists
}

func (c *realtimeUsageCollector) flushPending() {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.pending {
		addRealtimeUsage(&c.total, &c.pending[i])
	}
	c.pending = nil
}

func (c *realtimeUsageCollector) snapshot() dto.RealtimeUsage {
	if c == nil {
		return dto.RealtimeUsage{}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

func addRealtimeUsage(dst, src *dto.RealtimeUsage) {
	if dst == nil || src == nil {
		return
	}

	dst.TotalTokens += src.TotalTokens
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.InputTokenDetails.AudioTokens += src.InputTokenDetails.AudioTokens
	dst.InputTokenDetails.CachedTokens += src.InputTokenDetails.CachedTokens
	dst.InputTokenDetails.CachedCreationTokens += src.InputTokenDetails.CachedCreationTokens
	dst.InputTokenDetails.CacheWriteTokens += src.InputTokenDetails.CacheWriteTokens
	dst.InputTokenDetails.ImageTokens += src.InputTokenDetails.ImageTokens
	dst.InputTokenDetails.TextTokens += src.InputTokenDetails.TextTokens
	dst.OutputTokenDetails.AudioTokens += src.OutputTokenDetails.AudioTokens
	dst.OutputTokenDetails.ImageTokens += src.OutputTokenDetails.ImageTokens
	dst.OutputTokenDetails.ReasoningTokens += src.OutputTokenDetails.ReasoningTokens
	dst.OutputTokenDetails.TextTokens += src.OutputTokenDetails.TextTokens
}

func OpenaiRealtimeHandler(c *gin.Context, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.RealtimeUsage) {
	if info == nil || info.ClientWs == nil || info.TargetWs == nil {
		return types.NewError(fmt.Errorf("invalid websocket connection"), types.ErrorCodeBadResponse), nil
	}

	info.IsStream = true
	clientConn := info.ClientWs
	targetConn := info.TargetWs

	readerDone := make(chan struct{}, 2)
	errChan := make(chan error, 2)
	collector := newRealtimeUsageCollector()
	var handlerErr error
	var stopping atomic.Bool

	// RelayInfo is shared by both readers for realtime audio formats, tools and
	// IsFirstRequest. Keep those fields synchronized with the usage collector.
	var stateMu sync.RWMutex
	countTokens := func(event dto.RealtimeEvent) (int, int, error) {
		stateMu.RLock()
		defer stateMu.RUnlock()
		return service.CountTokenRealtime(info, event, info.UpstreamModelName)
	}

	var readers sync.WaitGroup
	readers.Add(2)
	stopConnections := sync.OnceFunc(func() {
		stopping.Store(true)
		// Closing both sockets unblocks a reader that is waiting in ReadMessage.
		_ = clientConn.Close()
		_ = targetConn.Close()
	})

	startReader := func(reader func()) {
		gopool.Go(func() {
			defer readers.Done()
			defer func() {
				if r := recover(); r != nil {
					errChan <- fmt.Errorf("panic in realtime reader: %v", r)
				}
				readerDone <- struct{}{}
			}()
			reader()
		})
	}

	startReader(func() {
		for {
			_, message, err := clientConn.ReadMessage()
			if err != nil {
				if !stopping.Load() && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					errChan <- fmt.Errorf("error reading from client: %v", err)
				}
				return
			}

			realtimeEvent := &dto.RealtimeEvent{}
			if err = common.Unmarshal(message, realtimeEvent); err != nil {
				errChan <- fmt.Errorf("error unmarshalling client message: %v", err)
				return
			}
			if realtimeEvent.Type == dto.RealtimeEventTypeResponseCreate {
				collector.startResponse()
			}

			if realtimeEvent.Type == dto.RealtimeEventTypeSessionUpdate && realtimeEvent.Session != nil && realtimeEvent.Session.Tools != nil {
				stateMu.Lock()
				info.RealtimeTools = realtimeEvent.Session.Tools
				stateMu.Unlock()
			}

			textToken, audioToken, err := countTokens(*realtimeEvent)
			if err != nil {
				errChan <- fmt.Errorf("error counting client realtime token: %v", err)
				return
			}
			logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
			collector.addLocal(textToken, audioToken, true)

			if err = helper.WssString(c, targetConn, string(message)); err != nil {
				errChan <- fmt.Errorf("error writing to target: %v", err)
				return
			}
		}
	})

	startReader(func() {
		for {
			_, message, err := targetConn.ReadMessage()
			if err != nil {
				if !stopping.Load() && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					errChan <- fmt.Errorf("error reading from target: %v", err)
				}
				return
			}
			info.SetFirstResponseTime()

			realtimeEvent := &dto.RealtimeEvent{}
			if err = common.Unmarshal(message, realtimeEvent); err != nil {
				errChan <- fmt.Errorf("error unmarshalling target message: %v", err)
				return
			}

			switch realtimeEvent.Type {
			case dto.RealtimeEventTypeResponseDone:
				// A duplicate response.done must not add its tool-token estimate
				// again. Skip counting the event itself, while finishResponse keeps
				// any pending estimate that belongs to a later response intact.
				if !collector.responseSeen(realtimeEvent.EventId) {
					textToken, audioToken, err := countTokens(*realtimeEvent)
					if err != nil {
						errChan <- fmt.Errorf("error counting response token: %v", err)
						return
					}
					logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
					// Tool tokens are input tokens and are only used when the provider
					// omits response usage.
					collector.addResponseLocal(textToken, audioToken, true)
				}
				var providerUsage *dto.RealtimeUsage
				if realtimeEvent.Response != nil {
					providerUsage = realtimeEvent.Response.Usage
				}
				collector.finishResponse(realtimeEvent.EventId, providerUsage)
				stateMu.Lock()
				info.IsFirstRequest = false
				stateMu.Unlock()
			case dto.RealtimeEventTypeError:
				errorMessage := "upstream realtime error"
				if realtimeEvent.Error != nil && realtimeEvent.Error.Message != "" {
					errorMessage = realtimeEvent.Error.Message
				}
				// Forward the provider error event before stopping the reader so
				// websocket clients still receive the upstream diagnostic.
				if err = helper.WssString(c, clientConn, string(message)); err != nil {
					errChan <- fmt.Errorf("error writing upstream realtime error to client: %v", err)
					return
				}
				errChan <- fmt.Errorf("upstream realtime error: %s", errorMessage)
				return

			case dto.RealtimeEventTypeSessionUpdated, dto.RealtimeEventTypeSessionCreated:
				if realtimeEvent.Session != nil {
					stateMu.Lock()
					info.InputAudioFormat = common.GetStringIfEmpty(realtimeEvent.Session.InputAudioFormat, info.InputAudioFormat)
					info.OutputAudioFormat = common.GetStringIfEmpty(realtimeEvent.Session.OutputAudioFormat, info.OutputAudioFormat)
					stateMu.Unlock()
				}

			default:
				textToken, audioToken, err := countTokens(*realtimeEvent)
				if err != nil {
					errChan <- fmt.Errorf("error counting target realtime token: %v", err)
					return
				}
				logger.LogInfo(c, fmt.Sprintf("type: %s, textToken: %d, audioToken: %d", realtimeEvent.Type, textToken, audioToken))
				collector.addResponseLocal(textToken, audioToken, false)
			}

			if err = helper.WssString(c, clientConn, string(message)); err != nil {
				errChan <- fmt.Errorf("error writing to client: %v", err)
				return
			}
		}
	})

	select {
	case <-readerDone:
	case err := <-errChan:
		handlerErr = err
		logger.LogError(c, "realtime error: "+err.Error())
	case <-c.Done():
		if err := c.Err(); err != nil {
			handlerErr = fmt.Errorf("realtime request cancelled: %w", err)
		} else {
			handlerErr = fmt.Errorf("realtime request cancelled")
		}
	}
	stopConnections()
	readers.Wait()

	for {
		select {
		case err := <-errChan:
			if handlerErr == nil {
				handlerErr = err
			}
			logger.LogError(c, "realtime error: "+err.Error())
		default:
			collector.flushPending()
			usage := collector.snapshot()
			if handlerErr != nil {
				return types.NewError(handlerErr, types.ErrorCodeBadResponse), &usage
			}
			return nil, &usage
		}
	}
}

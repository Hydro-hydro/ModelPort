package openai

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestRealtimeUsageCollectorProviderUsageOverridesLocalEstimate(t *testing.T) {
	collector := newRealtimeUsageCollector()

	collector.addLocal(100, 10, true)
	collector.addLocal(20, 5, false)
	collector.finishResponse("response-1", &dto.RealtimeUsage{
		TotalTokens:  12,
		InputTokens:  7,
		OutputTokens: 5,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens:   4,
			AudioTokens:  2,
			CachedTokens: 1,
		},
		OutputTokenDetails: dto.OutputTokenDetails{
			TextTokens:  3,
			AudioTokens: 2,
		},
	})

	got := collector.snapshot()
	require.Equal(t, 12, got.TotalTokens)
	require.Equal(t, 7, got.InputTokens)
	require.Equal(t, 5, got.OutputTokens)
	require.Equal(t, 4, got.InputTokenDetails.TextTokens)
	require.Equal(t, 2, got.InputTokenDetails.AudioTokens)
	require.Equal(t, 1, got.InputTokenDetails.CachedTokens)
	require.Equal(t, 3, got.OutputTokenDetails.TextTokens)
	require.Equal(t, 2, got.OutputTokenDetails.AudioTokens)
}

func TestRealtimeUsageCollectorUsesLocalEstimateForEmptyProviderUsage(t *testing.T) {
	collector := newRealtimeUsageCollector()
	collector.addLocal(8, 2, true)
	collector.addLocal(3, 1, false)
	collector.finishResponse("response-empty-usage", &dto.RealtimeUsage{})

	got := collector.snapshot()
	require.Equal(t, 14, got.TotalTokens)
	require.Equal(t, 10, got.InputTokens)
	require.Equal(t, 4, got.OutputTokens)
}

func TestRealtimeUsageCollectorUsesLocalEstimateWhenProviderOmitsUsage(t *testing.T) {
	collector := newRealtimeUsageCollector()

	collector.addLocal(4, 2, true)
	collector.addLocal(3, 1, false)
	collector.finishResponse("response-1", nil)
	collector.addLocal(8, 0, true)
	collector.finishResponse("response-2", nil)

	got := collector.snapshot()
	require.Equal(t, 18, got.TotalTokens)
	require.Equal(t, 14, got.InputTokens)
	require.Equal(t, 4, got.OutputTokens)
	require.Equal(t, 12, got.InputTokenDetails.TextTokens)
	require.Equal(t, 2, got.InputTokenDetails.AudioTokens)
	require.Equal(t, 3, got.OutputTokenDetails.TextTokens)
	require.Equal(t, 1, got.OutputTokenDetails.AudioTokens)
}

func TestRealtimeUsageCollectorDeduplicatesProviderUsage(t *testing.T) {
	collector := newRealtimeUsageCollector()
	providerUsage := &dto.RealtimeUsage{TotalTokens: 9, InputTokens: 6, OutputTokens: 3}

	collector.finishResponse("response-1", providerUsage)
	collector.finishResponse("response-1", providerUsage)
	collector.flushPending()

	got := collector.snapshot()
	require.Equal(t, 9, got.TotalTokens)
	require.Equal(t, 6, got.InputTokens)
	require.Equal(t, 3, got.OutputTokens)
}

func TestRealtimeUsageCollectorDeduplicatesEstimatedUsage(t *testing.T) {
	collector := newRealtimeUsageCollector()

	collector.addLocal(4, 0, true)
	collector.addLocal(2, 0, false)
	collector.finishResponse("response-1", nil)
	collector.finishResponse("response-1", nil)
	collector.flushPending()

	got := collector.snapshot()
	require.Equal(t, 6, got.TotalTokens)
	require.Equal(t, 4, got.InputTokens)
	require.Equal(t, 2, got.OutputTokens)
}

func TestRealtimeUsageCollectorDuplicateDoneKeepsNextResponseEstimate(t *testing.T) {
	collector := newRealtimeUsageCollector()

	collector.addLocal(4, 0, true)
	collector.finishResponse("response-1", nil)
	// The next response has started before a delayed duplicate response.done
	// for response-1 arrives. The duplicate must not erase this pending usage.
	collector.addLocal(7, 0, true)
	collector.finishResponse("response-1", nil)
	collector.finishResponse("response-2", nil)

	got := collector.snapshot()
	require.Equal(t, 11, got.TotalTokens)
	require.Equal(t, 11, got.InputTokens)
}

func TestRealtimeUsageCollectorSeparatesOverlappingResponseEstimates(t *testing.T) {
	collector := newRealtimeUsageCollector()

	// The next response can be opened and receive local input before the
	// previous response.done arrives. Provider usage must replace only the
	// first response's estimate, leaving the second response to be estimated
	// locally when it completes.
	collector.startResponse()
	collector.addLocal(4, 0, true)
	collector.startResponse()
	collector.addLocal(7, 0, true)
	collector.finishResponse("response-1", &dto.RealtimeUsage{
		TotalTokens: 2,
		InputTokens: 2,
	})
	collector.finishResponse("response-2", nil)

	got := collector.snapshot()
	require.Equal(t, 9, got.TotalTokens)
	require.Equal(t, 9, got.InputTokens)
}

func TestRealtimeUsageCollectorAssignsDoneFallbackToOldestResponse(t *testing.T) {
	collector := newRealtimeUsageCollector()
	collector.startResponse()
	collector.addLocal(7, 0, true)
	collector.startResponse()
	collector.addLocal(11, 0, true)
	collector.addResponseLocal(3, 0, true)
	collector.finishResponse("response-1", nil)
	collector.finishResponse("response-2", nil)

	got := collector.snapshot()
	require.Equal(t, 21, got.TotalTokens)
	require.Equal(t, 21, got.InputTokens)
}

func TestRealtimeUsageCollectorKeepsDistinctResponsesWithoutEventID(t *testing.T) {
	collector := newRealtimeUsageCollector()

	// Providers are expected to send event_id, but a few compatible gateways
	// omit it. An empty ID cannot identify a duplicate: two response.done
	// events may still represent two separate responses and must both be
	// billed. Only non-empty IDs participate in de-duplication.
	collector.finishResponse("", &dto.RealtimeUsage{TotalTokens: 4, InputTokens: 3, OutputTokens: 1})
	collector.finishResponse("", &dto.RealtimeUsage{TotalTokens: 6, InputTokens: 4, OutputTokens: 2})

	got := collector.snapshot()
	require.Equal(t, 10, got.TotalTokens)
	require.Equal(t, 7, got.InputTokens)
	require.Equal(t, 3, got.OutputTokens)
}

func TestRealtimeUsageCollectorConcurrentLocalUsage(t *testing.T) {
	collector := newRealtimeUsageCollector()
	const goroutineCount = 4
	const additionsPerGoroutine = 10

	var waitGroup sync.WaitGroup
	waitGroup.Add(goroutineCount)
	for i := 0; i < goroutineCount; i++ {
		go func() {
			defer waitGroup.Done()
			for j := 0; j < additionsPerGoroutine; j++ {
				collector.addLocal(1, 1, true)
			}
		}()
	}
	waitGroup.Wait()
	collector.flushPending()

	got := collector.snapshot()
	require.Equal(t, goroutineCount*additionsPerGoroutine*2, got.TotalTokens)
	require.Equal(t, goroutineCount*additionsPerGoroutine*2, got.InputTokens)
	require.Zero(t, got.OutputTokens)
}

package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// Unsupported OpenAI resource APIs must stay unregistered until they have a
// real implementation. Registering a placeholder route makes clients believe
// the resource is supported and can also bypass the normal API 404 response.
func TestSetRelayRouterDoesNotRegisterUnsupportedOpenAIEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)

	routes := make(map[string]struct{}, len(engine.Routes()))
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	unsupported := []string{
		http.MethodGet + " /v1/fine-tunes",
		http.MethodPost + " /v1/fine-tunes",
		http.MethodGet + " /v1/fine-tunes/:fine_tune_id",
		http.MethodPost + " /v1/fine-tunes/:fine_tune_id/cancel",
		http.MethodGet + " /v1/fine-tunes/:fine_tune_id/events",
		http.MethodGet + " /v1/files",
		http.MethodPost + " /v1/files",
		http.MethodGet + " /v1/files/:file_id",
		http.MethodDelete + " /v1/files/:file_id",
		http.MethodGet + " /v1/files/:file_id/content",
		http.MethodPost + " /v1/images/variations",
	}

	for _, route := range unsupported {
		_, registered := routes[route]
		assert.False(t, registered, route)
	}
}

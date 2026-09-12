package server

import (
	"diffractllm/internal/providers"
	"net/http"
)

func (ds *DiffractLLMServer) GenericRequestHandler(w http.ResponseWriter, r *http.Request, desc *providers.RouteDescriptor) {
	// Context Pool

	// Run Authenticate here

	// Read the body

	// Get the descriptor to create a new request , unmarshal our current one to the body here

	//Conver to the internal Diffract LLM request

	// Read the body and figure out the model


	// Run PRE-CALL HOOKS

	// DATAPLANE - SELECTION ENGINE

	// CATALOG PRICING ENGINE - FIND OUT

	// GET THE PROVIDER INSTANCE

	//

}

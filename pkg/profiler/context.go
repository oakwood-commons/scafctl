package profiler

import (
	"context"
)

type profilerContextKey struct{}

var functionCallDetails = &FunctionCallDetailsList{}

// IntoContext returns a new context with the provided FunctionCallDetails instance.
// If the context already contains the same FunctionCallDetails instance, it returns the original context.
// Otherwise, it associates the provided FunctionCallDetails instance with the context.
//
// Parameters:
//
//	ctx - The original context.
//	functionCallDetails - The FunctionCallDetails instance to associate with the context.
//
// Returns:
//
//	A new context with the FunctionCallDetails instance, or the original context if it already contains the same FunctionCallDetails instance.
func IntoContext(ctx context.Context, functionCallDetails *FunctionCallDetailsList) context.Context {
	if lp, ok := ctx.Value(profilerContextKey{}).(*FunctionCallDetailsList); ok {
		if lp == functionCallDetails {
			return ctx
		}
	}
	return context.WithValue(ctx, profilerContextKey{}, functionCallDetails)
}

// FunctionCallDetailsFromCtx retrieves the FunctionCallDetails instance from the provided context.
// It first attempts to extract FunctionCallDetails from the context using profilerContextKey.
// If FunctionCallDetails is not found in the context, it falls back to the global functionCallDetails variable.
// Returns a pointer to FunctionCallDetails if found, otherwise returns nil.
//
// Parameters:
//
//	ctx - The context from which to retrieve FunctionCallDetails.
//
// Returns:
//
//	*FunctionCallDetails - A pointer to FunctionCallDetails if found, otherwise nil.
func FunctionCallDetailsFromCtx(ctx context.Context) *FunctionCallDetailsList {
	if res, ok := ctx.Value(profilerContextKey{}).(*FunctionCallDetailsList); ok {
		return res
	} else if res := functionCallDetails; res != nil {
		return res
	}
	return nil
}

func AddFunctionCallDetails(ctx context.Context, details FunctionCallDetails) {
	if ctx == nil {
		ctx = context.Background()
	}
	if res, ok := ctx.Value(profilerContextKey{}).(*FunctionCallDetailsList); ok {
		*res = append(*res, details)
	} else if res := functionCallDetails; res != nil {
		*res = append(*res, details)
	}
}

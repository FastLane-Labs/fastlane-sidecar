package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

	jsonApiRpc "github.com/FastLane-Labs/fastlane-json-rpc/rpc"
	"github.com/FastLane-Labs/fastlane-sidecar/log"
	logtracker "github.com/FastLane-Labs/fastlane-sidecar/log_tracker"
)

type SidecarApi struct {
	apiFunctionRegister map[string]reflect.Value
	logTracker          *logtracker.LogTracker
}

func NewSidecarApi(logTracker *logtracker.LogTracker) *SidecarApi {
	api := &SidecarApi{
		apiFunctionRegister: make(map[string]reflect.Value),
		logTracker:          logTracker,
	}

	api.RegisterRpcMethod("streamLogs", reflect.ValueOf(api.streamLogs))

	return api
}

func (api *SidecarApi) RegisterRpcMethod(methodName string, method reflect.Value) error {
	if _, ok := api.apiFunctionRegister[methodName]; ok {
		return fmt.Errorf("method %s already registered", methodName)
	}
	api.apiFunctionRegister[methodName] = method
	return nil
}

func (api *SidecarApi) RuntimeMethod(methodName string) reflect.Value {
	if method, ok := api.apiFunctionRegister[methodName]; !ok {
		return reflect.Value{}
	} else {
		return method
	}
}

func (api *SidecarApi) streamLogs(ctx context.Context, filter string) {
	conn, err := getConnFromContext(ctx)
	if err != nil {
		log.Error("streamLogs", "error", err)
		return
	}

	api.logTracker.RegisterCallback(logtracker.LogType(0), func(line string) {
		msg := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "logUpdate",
			"params":  []any{line},
		}
		json, err := json.Marshal(msg)
		if err != nil {
			fmt.Println("streamLogs: error marshalling notification:", err)
			return
		}
		conn.SendRaw(json)
	})

}
func (s *Sidecar) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func getConnFromContext(ctx context.Context) (*jsonApiRpc.Conn, error) {
	fmt.Println("getConnFromContext", ctx)
	val := ctx.Value(jsonApiRpc.ConnContextKey)
	if conn, ok := val.(*jsonApiRpc.Conn); ok {
		return conn, nil
	}
	return nil, fmt.Errorf("WebSocket connection not found in context (probably HTTP request)")
}

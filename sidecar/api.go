package sidecar

import (
	"fmt"
	"net/http"
	"reflect"
)

type SidecarApi struct {
	apiFunctionRegister map[string]reflect.Value
}

func NewSidecarApi() *SidecarApi {
	api := &SidecarApi{
		apiFunctionRegister: make(map[string]reflect.Value),
	}

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

func (s *Sidecar) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}


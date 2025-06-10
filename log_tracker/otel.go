package logtracker

import (
	"context"
	"fmt"
	"net"
	"sync"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/grpc"
)

type OTelLogTracker struct {
	collogspb.UnimplementedLogsServiceServer
	parser    LogParser
	callbacks map[LogType][]LogCallback
	stopCh    chan struct{}
	wg        sync.WaitGroup
	listener  net.Listener
	grpcSrv   *grpc.Server
}

func NewOTelLogTracker(listenAddr string) (*OTelLogTracker, error) {
	lt := &OTelLogTracker{
		parser:    &NilParser{},
		callbacks: make(map[LogType][]LogCallback),
		stopCh:    make(chan struct{}),
	}
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}
	lt.listener = lis
	lt.grpcSrv = grpc.NewServer()
	collogspb.RegisterLogsServiceServer(lt.grpcSrv, lt)
	return lt, nil
}

func (lt *OTelLogTracker) RegisterCallback(logType LogType, cb LogCallback) {
	lt.callbacks[logType] = append(lt.callbacks[logType], cb)
}

func (lt *OTelLogTracker) Start() {
	lt.wg.Add(1)
	go func() {
		defer lt.wg.Done()
		if err := lt.grpcSrv.Serve(lt.listener); err != nil {
			fmt.Println("grpc serve error:", err)
		}
	}()
}

func (lt *OTelLogTracker) Stop() {
	close(lt.stopCh)
	lt.grpcSrv.GracefulStop()
	lt.wg.Wait()
}

func (lt *OTelLogTracker) Export(ctx context.Context, req *collogspb.ExportLogsServiceRequest) (*collogspb.ExportLogsServiceResponse, error) {
	for _, rl := range req.ResourceLogs {
		for _, sl := range rl.ScopeLogs {
			for _, logRecord := range sl.LogRecords {
				content := logRecord.Body.GetStringValue()
				logTypes := lt.parser.ParseLog(content)
				for _, logType := range logTypes {
					for _, cb := range lt.callbacks[logType] {
						cb(content)
					}
				}
			}
		}
	}
	return &collogspb.ExportLogsServiceResponse{}, nil
}

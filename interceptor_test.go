package ratelimit

import (
	"context"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/conduitio/conn-rate-limit/proto"
	"github.com/golang/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestWithRateLimitedContextDialer(t *testing.T) {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	srv := grpc.NewServer()

	// use in-memory connection
	lis := bufconn.Listen(1024 * 1024)
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}

	// create and register simple mock server
	mockServer := proto.NewMockTestServiceServer(ctrl)
	mockServer.EXPECT().TestRPC(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, request *proto.TestRequest) (*proto.TestResponse, error) {
		return &proto.TestResponse{Code: 1}, nil
	})
	proto.RegisterTestServiceServer(srv, mockServer)

	// start gRPC server
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.Serve(lis); err != nil {
			log.Fatalf("Server exited with error: %v", err)
		}
	}()
	defer func() {
		srv.Stop()
		wg.Wait()
	}()

	// open rate limited client connection, limited to 10 B/s
	conn, err := grpc.DialContext(ctx,
		"bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// this interceptor limits the bandwith
		WithRateLimitedContextDialer(10, 0, dialer),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()

	// create gRPC service client and measure how long it takes to get a response
	c := proto.NewTestServiceClient(conn)
	before := time.Now()
	resp, err := c.TestRPC(ctx, &proto.TestRequest{Id: "abcdefghijklmnopqrstuvwxyz"})
	elapsed := time.Since(before)
	if err != nil {
		t.Fatalf("Failed to call TestRPC: %v", err)
	}

	t.Log(resp)
	t.Log(elapsed) // it takes ~15 seconds, since we need to write 155 bytes and are rate limited to 10 B/s
}

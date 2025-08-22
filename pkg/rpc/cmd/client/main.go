package main

import (
	"context"
	"fmt" // Added for fmt.Errorf
	"log"
	"os"
	"time"

	pb "mcp-gateway/pkg/rpc/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultName = "world"
)

// callSayHello encapsulates the gRPC client connection and call logic.
func callSayHello(name string) error {
	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("did not connect: %v", err)
	}
	defer conn.Close() // This defer will now run at the end of callSayHello

	c := pb.NewGreeterClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := c.SayHello(ctx, &pb.HelloRequest{Name: name})
	if err != nil {
		return fmt.Errorf("could not greet: %v", err)
	}
	log.Printf("Greeting: %s", r.GetMessage())
	return nil
}

func main() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		name := defaultName
		if len(os.Args) > 1 {
			name = os.Args[1]
		}
		if err := callSayHello(name); err != nil {
			log.Printf("Error in SayHello: %v", err) // Log the error instead of fatal
		}
	}
}

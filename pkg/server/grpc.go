package server

import (
	"context"
	"net"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/apex/log"
	"github.com/docker/docker/client"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/node-isp/node-isp/pkg/grpc"
	"github.com/node-isp/node-isp/pkg/service"
	"github.com/node-isp/node-isp/pkg/updater"
)

type grpcServer struct {
	pb.UnimplementedNodeISPServiceServer

	u      *updater.Updater
	mgr    *service.Manager
	docker *client.Client
}

// Run starts the gRPC server in a goroutine.
func (s *grpcServer) Run() error {
	lis, err := net.Listen("tcp", ":50051")

	srv := grpc.NewServer()

	pb.RegisterNodeISPServiceServer(srv, s)

	if err != nil {
		log.WithField("component", "grpc").WithError(err).Error("failed to listen")
		return err
	}

	go func() {
		if err := srv.Serve(lis); err != nil {
			log.WithField("component", "grpc").WithError(err).Error("failed to serve")
		}
	}()

	return nil
}

func (s *grpcServer) GetVersion(_ context.Context, _ *pb.GetVersionRequest) (*pb.GetVersionResponse, error) {
	currentVersion, err := semver.NewVersion(updater.CurrentAppVersion)
	if err != nil {
		return nil, err
	}

	latestVersion, err := s.u.LatestAppVersion()
	if err != nil {
		return nil, err
	}

	return &pb.GetVersionResponse{
		CurrentVersion:  currentVersion.String(),
		LatestVersion:   latestVersion.String(),
		UpdateAvailable: latestVersion.GreaterThan(currentVersion),
	}, nil
}

// GetStatus returns the current status of the NodeISP server and all of its services.
func (s *grpcServer) GetStatus(ctx context.Context, _ *pb.GetStatusRequest) (*pb.GetStatusResponse, error) {
	ctrs, err := s.mgr.ListContainers(ctx)
	if err != nil {
		return nil, err
	}

	var services []*pb.Service

	for _, svc := range s.mgr.Services {
		// Check if the service is running
		var state, container string
		var started time.Time

		for _, ctr := range ctrs {
			// Find the container with a label matching the service name
			if ctr.Labels["service"] == svc.Name {
				state = ctr.State
				container = ctr.Names[0]
				started = time.Unix(ctr.Created, 0)
			}
		}

		// convert the service to a protobuf service
		services = append(services, &pb.Service{
			Name:      svc.Name,
			Container: container,
			Image:     svc.Image,
			Status:    state,
			Started:   timestamppb.New(started),
		})
	}

	return &pb.GetStatusResponse{Services: services}, nil
}
